package pkg

// LockFreePool 无锁的连接级别 Buffer Pool
// 设计说明：
// - 基于连接，单线程使用，无需加锁
// - 固定大小 Buffer（4KB）
// - 支持动态扩容 grow
// - 使用链表管理空闲 Buffer
type LockFreePool struct {
	free *Buffer // 空闲链表头
	num  int     // 每次 grow 增加的 Buffer 数量
	size int     // 每个 Buffer 的大小（固定 4KB）
}

// NewLockFreePool 创建无锁 Buffer Pool
// num: 初始 Buffer 数量
// size: 每个 Buffer 大小（通常是 4096）
func NewLockFreePool(num, size int) *LockFreePool {
	p := &LockFreePool{
		num:  num,
		size: size,
	}
	p.grow()
	return p
}

// grow 扩容：申请 num 个新的 Buffer
func (p *LockFreePool) grow() {
	// 申请一块连续内存
	totalSize := p.num * p.size
	buf := make([]byte, totalSize)
	
	// 创建 Buffer 结构体数组
	buffers := make([]Buffer, p.num)
	
	// 链表串联
	for i := 0; i < p.num; i++ {
		buffers[i].buf = buf[i*p.size : (i+1)*p.size]
		
		if i < p.num-1 {
			buffers[i].next = &buffers[i+1]
		} else {
			// 最后一个 Buffer 指向当前的空闲链表头
			buffers[i].next = p.free
		}
	}
	
	// 更新空闲链表头
	p.free = &buffers[0]
}

// Get 从池中获取一个 Buffer
// 如果池为空，自动扩容
func (p *LockFreePool) Get() *Buffer {
	if p.free == nil {
		p.grow()
	}
	
	b := p.free
	p.free = b.next
	b.next = nil
	return b
}

// Put 归还 Buffer 到池中（头插法）
func (p *LockFreePool) Put(b *Buffer) {
	b.next = p.free
	p.free = b
}
