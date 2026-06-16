package getty

import (
	"encoding/binary"
	"errors"
	getty "github.com/AlexStocks/getty/transport"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"log"
	"sync"
)

var (
	ErrNotEnoughStream = errors.New("not enough stream")
)

// readBufferSize 是 read buffer 的固定大小（4KB）。
// 如果消息超过这个大小，buffer 会按需重新分配（不放回池）。
const readBufferSize = 4096

// readBufferPool 是 server-level 共享的 read buffer 池。
//
// 设计说明：
// - 跨连接共享：所有 Read 操作从同一个池获取 buffer，per-P 缓存几乎无锁。
// - GC 友好：runtime 自动管理生命周期，空闲时回收，繁忙时保留。
// - 替代 LockFreePool：原 LockFreePool 在 Read/Release 跨 goroutine 调用下有 race。
var readBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, readBufferSize)
		return &buf
	},
}

// ProtoWithBuffer 绑定 Proto 和它使用的 Buffer。
// 使用完后必须调用 Release() 归还 buffer 到池。
type ProtoWithBuffer struct {
	Proto *protocol.Proto
	buf   *[]byte // 指向 sync.Pool 中的 buffer，Release 时归还
}

// Release 归还 Buffer 到 server-level 池。
// 多次调用安全（buf 设为 nil 后第二次调用是 noop）。
func (p *ProtoWithBuffer) Release() {
	if p.buf != nil {
		readBufferPool.Put(p.buf)
		p.buf = nil
		// 切断 Proto.Body 对池中 buffer 的引用，避免使用方持续访问已归还内存
		if p.Proto != nil {
			p.Proto.Body = nil
		}
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

// ProtoPackageHandler 处理 protocol.Proto 的 PackageHandler。
// Buffer 通过 server-level sync.Pool 管理（readBufferPool）。
type ProtoPackageHandler struct{}

// NewProtoPackageHandler 创建新的 ProtoPackageHandler。
// Buffer 通过 server-level sync.Pool（readBufferPool）管理，所有连接共享。
func NewProtoPackageHandler() *ProtoPackageHandler {
	return &ProtoPackageHandler{}
}

// Close 清理资源（无需操作，buffer 池由 runtime GC 管理）。
func (h *ProtoPackageHandler) Close() {}

// Read 从 data []byte 中解析 protocol.Proto（零拷贝 + sync.Pool）。
//
// 协议格式：[packLen(4)] [headerLen(2)] [Ver(2)] [Op(4)] [Seq(4)] [RoomIdLen(2)] [RoomId(...)] [UserIdLen(2)] [UserId(...)] [Body(...)]
//
// Buffer 生命周期：
// 1. 从 server-level readBufferPool 取一块 buffer（per-P 缓存命中几乎无锁）。
// 2. Proto.Body 零拷贝引用 buffer 内存。
// 3. 调用方处理完后必须调用 ProtoWithBuffer.Release() 归还 buffer。
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

	// 从 server-level sync.Pool 获取 buffer（per-P 本地缓存，几乎无锁）
	bufp := readBufferPool.Get().(*[]byte)
	bufBytes := *bufp

	// 释放路径：解析失败时归还 buffer，避免泄漏
	releaseOnError := func() {
		readBufferPool.Put(bufp)
	}

	// 将数据复制到 buffer
	copy(bufBytes, data)

	// 读取 packLen（总包长度，4字节，大端序）
	packLen = int32(binary.BigEndian.Uint32(bufBytes[_packOffset:_headerOffset]))

	// 检查 packLen 是否合理
	if packLen < 0 || packLen > _maxPackSize {
		releaseOnError()
		return nil, 0, protocol.ErrProtoPackLen
	}

	// 检查是否有足够的数据
	if len(data) < int(packLen) {
		releaseOnError()
		return nil, 0, ErrNotEnoughStream
	}

	// 如果 buffer 太小，按需扩容（不放回池，避免污染池中固定大小的 buffer）
	if len(bufBytes) < int(packLen) {
		newBuf := make([]byte, packLen)
		copy(newBuf, data[:packLen])
		// 旧 buffer 归还到池
		readBufferPool.Put(bufp)
		bufBytes = newBuf
		// 临时容器：超大 buffer 不放回池（Release 时丢弃即可）
		bufp = &newBuf
	}

	// 读取 headerLen（header 长度，2字节，大端序）
	headerLen = int16(binary.BigEndian.Uint16(bufBytes[_headerOffset:_verOffset]))
	if headerLen != _rawHeaderSize {
		readBufferPool.Put(bufp)
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
		readBufferPool.Put(bufp)
		return nil, 0, ErrNotEnoughStream
	}
	roomIdLen = int16(binary.BigEndian.Uint16(bufBytes[roomIdOffset : roomIdOffset+_stringLenSize]))
	if roomIdLen > 0 {
		roomIdDataOffset := roomIdOffset + _stringLenSize
		if len(bufBytes) < roomIdDataOffset+int(roomIdLen) {
			readBufferPool.Put(bufp)
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
		readBufferPool.Put(bufp)
		return nil, 0, ErrNotEnoughStream
	}
	userIdLen = int16(binary.BigEndian.Uint16(bufBytes[userIdOffset : userIdOffset+_stringLenSize]))
	if userIdLen > 0 {
		userIdDataOffset := userIdOffset + _stringLenSize
		if len(bufBytes) < userIdDataOffset+int(userIdLen) {
			readBufferPool.Put(bufp)
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
		Proto: &pkg,
		buf:   bufp,
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
