package getty

import (
	"encoding/binary"
	"errors"
	getty "github.com/AlexStocks/getty/transport"
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"log"
	"time"
)

var (
	ErrNotEnoughStream = errors.New("not enough stream")
)

// 协议格式常量
const (
	_packSize      = 4
	_headerSize    = 2
	_verSize       = 2
	_opSize        = 4
	_seqSize       = 4
	_stringLenSize = 2 // string 字段的长度前缀大小
	_rawHeaderSize = _packSize + _headerSize + _verSize + _opSize + _seqSize // 16 字节（固定 header）
	_maxPackSize   = protocol.MaxBodySize + int32(_rawHeaderSize)
	// offset
	_packOffset   = 0
	_headerOffset = _packOffset + _packSize
	_verOffset    = _headerOffset + _headerSize
	_opOffset     = _verOffset + _verSize
	_seqOffset    = _opOffset + _opSize
)

// ProtoPackageHandler 处理 protocol.Proto 的 PackageHandler
// 设计说明（零拷贝优化）：
// - Read: 使用 session 级别的 ReadBuffer（不释放），Proto.Body 直接引用 buffer 内存
// - Write: 直接分配目标大小的 []byte 并写入，避免额外拷贝
// - ReadBuffer 会一直被复用（滑动窗口），所以消费者必须在 buffer 被覆盖前处理完数据
type ProtoPackageHandler struct {
	ReadBuffer *pkg.Buffer // session 级别的 read buffer（零拷贝，不归还）
	ReadPool   *pkg.Pool   // 用于获取 ReadBuffer（仅在创建时）
}

// NewProtoPackageHandler 创建新的 ProtoPackageHandler
// readBuffer 从 pool 获取一次，整个 session 生命周期内持有
// writePool 不再需要（Write 直接分配内存）
func NewProtoPackageHandler(readPool, writePool *pkg.Pool) *ProtoPackageHandler {
	return &ProtoPackageHandler{
		ReadBuffer: readPool.Get(), // 获取一次，session 结束前不归还
		ReadPool:   readPool,
	}
}

// Close 清理资源（session 关闭时归还 ReadBuffer）
func (h *ProtoPackageHandler) Close() {
	if h.ReadBuffer != nil && h.ReadPool != nil {
		h.ReadPool.Put(h.ReadBuffer)
		h.ReadBuffer = nil
	}
}

// Read 从 data []byte 中解析 protocol.Proto（零拷贝）
// 协议格式：[packLen(4)] [headerLen(2)] [Ver(2)] [Op(4)] [Seq(4)] [RoomIdLen(2)] [RoomId(...)] [UserIdLen(2)] [UserId(...)] [Body(...)]
// 
// 零拷贝优化：Proto.Body 直接引用 ReadBuffer 的内存，不进行拷贝
// 风险：ReadBuffer 会被持续复用，消费者必须在下次 Read 覆盖数据前处理完
// 
// 使用方式：
// 1. Read 解析后将 Proto 放入 CliProto Ring Buffer
// 2. dispatchWebsocket 从 Ring Buffer 取出并处理
// 3. 处理完后调用 GetAdv() (rp++)，允许 Ring Buffer 复用该位置
func (h *ProtoPackageHandler) Read(ss getty.Session, data []byte) (any, int, error) {
	log.Printf("🔍 [ProtoHandler] Read 被调用: dataLen=%d", len(data))
	
	var (
		bodyLen      int
		headerLen    int16
		packLen      int32
		roomIdLen    int16
		userIdLen    int16
		roomIdOffset int
		userIdOffset int
		bodyOffset   int
		pkg          protocol.Proto
	)

	// 检查是否有足够的字节读取 header（至少需要 _rawHeaderSize = 16 字节）
	if len(data) < _rawHeaderSize {
		log.Printf("⚠️  [ProtoHandler] 数据不足: len(data)=%d < _rawHeaderSize=%d", len(data), _rawHeaderSize)
		return nil, 0, ErrNotEnoughStream
	}

	// 使用 session 级别的 ReadBuffer（零拷贝）
	bufBytes := h.ReadBuffer.Bytes()

	// 将数据复制到 buffer
	copy(bufBytes, data)

	// 读取 packLen（总包长度，4字节，大端序）
	packLen = int32(binary.BigEndian.Uint32(bufBytes[_packOffset:_headerOffset]))

	// 检查 packLen 是否合理
	if packLen < 0 || packLen > _maxPackSize {
		return nil, 0, protocol.ErrProtoPackLen
	}

	// 检查是否有足够的数据
	if len(data) < int(packLen) {
		return nil, 0, ErrNotEnoughStream
	}

	// 确保 buffer 中有完整的数据
	if len(bufBytes) < int(packLen) {
		copy(bufBytes, data[:packLen])
	}

	// 读取 headerLen（header 长度，2字节，大端序）
	headerLen = int16(binary.BigEndian.Uint16(bufBytes[_headerOffset:_verOffset]))
	if headerLen != _rawHeaderSize {
		return nil, 0, protocol.ErrProtoHeaderLen
	}

	// 读取 Ver（版本，2字节，大端序）
	pkg.Ver = int32(binary.BigEndian.Uint16(bufBytes[_verOffset:_opOffset]))

	// 读取 Op（操作类型，4字节，大端序）
	pkg.Op = int32(binary.BigEndian.Uint32(bufBytes[_opOffset:_seqOffset]))

	// 读取 Seq（序列号，4字节，大端序）
	pkg.Seq = int32(binary.BigEndian.Uint32(bufBytes[_seqOffset : _seqOffset+_seqSize]))

	// 读取 RoomId（2字节长度 + UTF-8 数据）
	roomIdOffset = _seqOffset + _seqSize
	if len(bufBytes) < roomIdOffset+_stringLenSize {
		return nil, 0, ErrNotEnoughStream
	}
	roomIdLen = int16(binary.BigEndian.Uint16(bufBytes[roomIdOffset : roomIdOffset+_stringLenSize]))
	if roomIdLen > 0 {
		roomIdDataOffset := roomIdOffset + _stringLenSize
		if len(bufBytes) < roomIdDataOffset+int(roomIdLen) {
			return nil, 0, ErrNotEnoughStream
		}
		// string 会自动拷贝（Go 的 string 转换机制）
		pkg.Roomid = string(bufBytes[roomIdDataOffset : roomIdDataOffset+int(roomIdLen)])
	} else {
		pkg.Roomid = ""
	}

	// 读取 UserId（2字节长度 + UTF-8 数据）
	userIdOffset = roomIdOffset + _stringLenSize + int(roomIdLen)
	if len(bufBytes) < userIdOffset+_stringLenSize {
		return nil, 0, ErrNotEnoughStream
	}
	userIdLen = int16(binary.BigEndian.Uint16(bufBytes[userIdOffset : userIdOffset+_stringLenSize]))
	if userIdLen > 0 {
		userIdDataOffset := userIdOffset + _stringLenSize
		if len(bufBytes) < userIdDataOffset+int(userIdLen) {
			return nil, 0, ErrNotEnoughStream
		}
		// string 会自动拷贝（Go 的 string 转换机制）
		pkg.Userid = string(bufBytes[userIdDataOffset : userIdDataOffset+int(userIdLen)])
	} else {
		pkg.Userid = ""
	}

	// 读取 Body（零拷贝：直接引用 ReadBuffer 内存）
	bodyOffset = userIdOffset + _stringLenSize + int(userIdLen)
	bodyLen = int(packLen) - bodyOffset
	if bodyLen > 0 {
		// ⚠️  零拷贝：直接引用 bufBytes，不拷贝！
		// 消费者必须在下次 Read 前处理完该数据
		pkg.Body = bufBytes[bodyOffset : bodyOffset+bodyLen]
	} else {
		pkg.Body = nil
	}

	readLen := int(packLen)
	log.Printf("📥 [ProtoHandler] 读取 protocol.Proto: ver=%d, op=%d, seq=%d, roomId=%s, userId=%s, bodyLen=%d, totalLen=%d",
		pkg.Ver, pkg.Op, pkg.Seq, pkg.Roomid, pkg.Userid, bodyLen, readLen)

	return &pkg, readLen, nil
}

// Write 将 protocol.Proto 序列化为 []byte
// 协议格式：[packLen(4)] [headerLen(2)] [Ver(2)] [Op(4)] [Seq(4)] [RoomIdLen(2)] [RoomId(...)] [UserIdLen(2)] [UserId(...)] [Body(...)]
// 直接分配目标大小的 []byte 并写入，避免使用 buffer pool 的额外拷贝
func (h *ProtoPackageHandler) Write(ss getty.Session, pkg any) ([]byte, error) {
	var (
		ok           bool
		startTime    time.Time
		protoPkg     *protocol.Proto
		packLen      int
		roomIdLen    int
		userIdLen    int
		roomIdOffset int
		userIdOffset int
		bodyOffset   int
		result       []byte
	)

	startTime = time.Now()

	// 类型断言
	if protoPkg, ok = pkg.(*protocol.Proto); !ok {
		log.Printf("❌ [ProtoHandler] 非法包类型: %+v", pkg)
		return nil, errors.New("invalid protocol.Proto package")
	}

	// 计算字符串长度
	roomIdLen = len(protoPkg.Roomid)
	userIdLen = len(protoPkg.Userid)

	// 计算总包长度
	packLen = _rawHeaderSize + _stringLenSize + roomIdLen + _stringLenSize + userIdLen + len(protoPkg.Body)

	// 直接分配目标大小的 []byte
	result = make([]byte, packLen)

	// 写入 packLen（总包长度，4字节，大端序）
	binary.BigEndian.PutUint32(result[_packOffset:], uint32(packLen))

	// 写入 headerLen（header 长度，2字节，大端序）
	binary.BigEndian.PutUint16(result[_headerOffset:], uint16(_rawHeaderSize))

	// 写入 Ver（版本，2字节，大端序）
	binary.BigEndian.PutUint16(result[_verOffset:], uint16(protoPkg.Ver))

	// 写入 Op（操作类型，4字节，大端序）
	binary.BigEndian.PutUint32(result[_opOffset:], uint32(protoPkg.Op))

	// 写入 Seq（序列号，4字节，大端序）
	binary.BigEndian.PutUint32(result[_seqOffset:], uint32(protoPkg.Seq))

	// 写入 RoomId（2字节长度 + UTF-8 数据）
	roomIdOffset = _seqOffset + _seqSize
	binary.BigEndian.PutUint16(result[roomIdOffset:], uint16(roomIdLen))
	if roomIdLen > 0 {
		copy(result[roomIdOffset+_stringLenSize:], protoPkg.Roomid)
	}

	// 写入 UserId（2字节长度 + UTF-8 数据）
	userIdOffset = roomIdOffset + _stringLenSize + roomIdLen
	binary.BigEndian.PutUint16(result[userIdOffset:], uint16(userIdLen))
	if userIdLen > 0 {
		copy(result[userIdOffset+_stringLenSize:], protoPkg.Userid)
	}

	// 写入 Body（如果有）
	bodyOffset = userIdOffset + _stringLenSize + userIdLen
	if protoPkg.Body != nil && len(protoPkg.Body) > 0 {
		copy(result[bodyOffset:], protoPkg.Body)
	}

	log.Printf("📤 [ProtoHandler] 写入 protocol.Proto: ver=%d, op=%d, seq=%d, roomId=%s, userId=%s, bodyLen=%d, totalLen=%d, time=%v",
		protoPkg.Ver, protoPkg.Op, protoPkg.Seq, protoPkg.Roomid, protoPkg.Userid, len(protoPkg.Body), packLen, time.Since(startTime))

	return result, nil
}
