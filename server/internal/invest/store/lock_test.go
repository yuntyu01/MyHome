package store

import (
	"context"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 🔴 refresh가 재시도 사이에 15분씩(최대 90분) 기다리는 동안 잠금을 쥐고 있으면, 그 사이 도는 judge·주간
// 갱신이 10분 기다리다 75로 끝나 실패 알림이 간다. 기다리는 동안은 놓고, 끝나면 다시 잡아야 한다.
func TestHeldLockIsReleasedWhilePausing(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	// advisory lock은 DB 전체에 걸린다. 같은 DB에서 도는 다른 테스트와 부딪치지 않게 이 테스트만의 키를 쓴다
	key := time.Now().UnixNano()
	refresh, judge := New(pool, "invest refresh"), New(pool, "invest judge")
	refresh.lockID, judge.lockID = key, key

	held, err := refresh.Hold(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := judge.Lock(ctx, 200*time.Millisecond); err == nil {
		t.Fatal("잡혀 있는 잠금을 다른 명령이 잡았다")
	}
	var during error = context.Canceled
	if err := held.Pause(ctx, time.Minute, func(time.Duration) {
		u, err := judge.Lock(ctx, time.Second)
		if err == nil {
			u()
		}
		during = err
	}); err != nil {
		t.Fatalf("기다린 뒤 다시 잡지 못했다: %v", err)
	}
	if during != nil {
		t.Fatalf("기다리는 동안 다른 명령이 잠금을 못 잡았다: %v", during)
	}
	if _, err := judge.Lock(ctx, 200*time.Millisecond); err == nil {
		t.Fatal("다시 잡은 뒤인데 다른 명령이 잡았다")
	}
	held.Release()
	held.Release() // 두 번 불러도 된다
	u, err := judge.Lock(ctx, time.Second)
	if err != nil {
		t.Fatalf("풀었는데 못 잡는다: %v", err)
	}
	u()
}
