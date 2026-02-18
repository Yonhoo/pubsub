package getty

import (
	"encoding/binary"
	"errors"
	getty "github.com/AlexStocks/getty/transport"
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"log"
)

var (
	ErrNotEnoughStream = errors.New("not enough stream")
)

// ProtoWithBuffer 绑定 Proto 和它使用的 Buffer
// 使用完后需要调用 Release() 归还 Buffer
type ProtoWithBuffer struct {
	Proto  *protocol.Proto
	Buffer *pkg.Buffer
	pool   *pkg.LockFreePool
}

// Release 归还 Buffer 到池中
func (p *ProtoWithBuffer) Release() {
	if p.Buffer != nil && p.pool != nil {
		p.pool.Put(p.Buffer)
		p.Buffer = nil
	}
}

// 协议格式常量
const (
	_packSize      = 4
	_headerSize    = 2
	_verSize       = 2
	_opSize        = 4
	_seqSize       = 4
	_stringLenSize = 2                                                       // string 字段的长度前缀大小
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
// 设计说明（零拷贝 + Buffer Pool 优化）：
// - Read: 每次从连接级别的无锁 Pool 获取 Buffer，Proto.Body 直接引用 buffer 内存
// - Write: 直接分配目标大小的 []byte 并写入，避免额外拷贝
// - 使用完 Proto 后，调用 ProtoWithBuffer.Release() 归还 Buffer 到池中
type ProtoPackageHandler struct {
	BufferPool *pkg.LockFreePool // 连接级别的无锁 Buffer Pool（4KB 固定大小）
}

// NewProtoPackageHandler 创建新的 ProtoPackageHandler
// 基于连接的无锁 Buffer Pool（初始 8 个 Buffer，每个 4KB）
func NewProtoPackageHandler(readPool, writePool *pkg.Pool) *ProtoPackageHandler {
	const (
		initialBuffers = 8    // 初始 Buffer 数量
		bufferSize     = 4096 // 每个 Buffer 大小：4KB
	)

	return &ProtoPackageHandler{
		BufferPool: pkg.NewLockFreePool(initialBuffers, bufferSize),
	}
}

// Close 清理资源（无需操作，Pool 会被 GC 回收）
func (h *ProtoPackageHandler) Close() {
	// 连接级别的 Pool，连接关闭后会被 GC 回收
}

// Read 从 data []byte 中解析 protocol.Proto（零拷贝 + Buffer Pool）
// 协议格式：[packLen(4)] [headerLen(2)] [Ver(2)] [Op(4)] [Seq(4)] [RoomIdLen(2)] [RoomId(...)] [UserIdLen(2)] [UserId(...)] [Body(...)]
//
// 优化说明：
// 1. 每次 Read 从连接级别的无锁 Pool 获取 Buffer（4KB）
// 2. Proto.Body 零拷贝引用 Buffer 内存
// 3. 返回 ProtoWithBuffer，使用完后调用 Release() 归还 Buffer
//
// 使用方式：
// 1. Read 解析后将 ProtoWithBuffer 放入 Ring Buffer
// 2. dispatchWebsocket 从 Ring Buffer 取出并处理
// 3. 处理完后调用 ProtoWithBuffer.Release() 归还 Buffer
func (h *ProtoPackageHandler) Read(ss getty.Session, data []byte) (any, int, error) {

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

	// 从连接级别的无锁 Pool 获取 Buffer
	buf := h.BufferPool.Get()
	bufBytes := buf.Bytes()

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

	// 读取 Body（零拷贝：直接引用 Buffer 内存）
	bodyOffset = userIdOffset + _stringLenSize + int(userIdLen)
	bodyLen = int(packLen) - bodyOffset
	if bodyLen > 0 {
		// ⚠️  零拷贝：直接引用 bufBytes，不拷贝！
		// 使用完后必须调用 ProtoWithBuffer.Release() 归还 Buffer
		pkg.Body = bufBytes[bodyOffset : bodyOffset+bodyLen]
	} else {
		pkg.Body = nil
	}

	readLen := int(packLen)

	// 返回 ProtoWithBuffer（绑定 Proto 和 Buffer）
	return &ProtoWithBuffer{
		Proto:  &pkg,
		Buffer: buf,
		pool:   h.BufferPool,
	}, readLen, nil
}

// Write 将 protocol.Proto 序列化为 []byte
// 协议格式：[packLen(4)] [headerLen(2)] [Ver(2)] [Op(4)] [Seq(4)] [RoomIdLen(2)] [RoomId(...)] [UserIdLen(2)] [UserId(...)] [Body(...)]
// 直接分配目标大小的 []byte 并写入，避免使用 buffer pool 的额外拷贝
func (h *ProtoPackageHandler) Write(ss getty.Session, pkg any) ([]byte, error) {
	var (
		ok           bool
		protoPkg     *protocol.Proto
		packLen      int
		roomIdLen    int
		userIdLen    int
		roomIdOffset int
		userIdOffset int
		bodyOffset   int
		result       []byte
	)

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

	return result, nil
}
