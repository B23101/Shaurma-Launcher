package p2p

import "testing"

func TestLocalIPPool_AcquireDistinct(t *testing.T) {
	p := newLocalIPPool()
	ip1, err := p.acquire("room-a")
	if err != nil {
		t.Fatalf("acquire room-a: %v", err)
	}
	ip2, err := p.acquire("room-b")
	if err != nil {
		t.Fatalf("acquire room-b: %v", err)
	}
	if ip1 == ip2 {
		t.Fatalf("дві різні кімнати отримали однакову адресу: %s", ip1)
	}
	if ip1 == "127.0.0.1" || ip2 == "127.0.0.1" {
		t.Fatalf("пул не має видавати 127.0.0.1 (вона зарезервована під локальні потреби гравця)")
	}
}

func TestLocalIPPool_AcquireIdempotentForSameRoom(t *testing.T) {
	p := newLocalIPPool()
	ip1, err := p.acquire("room-a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ip2, err := p.acquire("room-a")
	if err != nil {
		t.Fatalf("повторний acquire: %v", err)
	}
	if ip1 != ip2 {
		t.Fatalf("повторний acquire тієї самої кімнати дав іншу адресу: %s vs %s", ip1, ip2)
	}
	if got := p.count(); got != 1 {
		t.Fatalf("count() = %d, want 1 (та сама кімната не має рахуватись двічі)", got)
	}
}

func TestLocalIPPool_ExhaustedAtHardLimit(t *testing.T) {
	p := newLocalIPPool()
	for i := 0; i < MaxConcurrentRooms; i++ {
		roomKey := string(rune('a' + i))
		if _, err := p.acquire(roomKey); err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
	}
	if _, err := p.acquire("one-too-many"); err != ErrPoolExhausted {
		t.Fatalf("очікувався ErrPoolExhausted після %d кімнат, отримали: %v", MaxConcurrentRooms, err)
	}
}

func TestLocalIPPool_ReleaseFreesSlot(t *testing.T) {
	p := newLocalIPPool()
	for i := 0; i < MaxConcurrentRooms; i++ {
		roomKey := string(rune('a' + i))
		if _, err := p.acquire(roomKey); err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
	}
	p.release("a")
	freedIP, err := p.acquire("new-room")
	if err != nil {
		t.Fatalf("acquire після release: %v", err)
	}
	if freedIP == "" {
		t.Fatalf("очікувалась звільнена адреса")
	}
	if got := p.count(); got != MaxConcurrentRooms {
		t.Fatalf("count() = %d, want %d", got, MaxConcurrentRooms)
	}
}

func TestLocalIPPool_ReleaseUnknownIsNoop(t *testing.T) {
	p := newLocalIPPool()
	p.release("nothing-acquired") // не має панікувати чи щось ламати
	if got := p.count(); got != 0 {
		t.Fatalf("count() = %d, want 0", got)
	}
}
