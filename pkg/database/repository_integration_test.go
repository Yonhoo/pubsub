package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openMySQLTestRepository(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("PUBSUB_MYSQL_TEST_DSN"))
	if dsn == "" {
		t.Skip("set PUBSUB_MYSQL_TEST_DSN to run MySQL integration tests")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(30 * time.Second)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return NewRepository(db), db
}

func cleanupRoom(t *testing.T, db *gorm.DB, roomID string) {
	t.Helper()

	if err := db.Exec("DELETE FROM room_users WHERE room_id = ?", roomID).Error; err != nil {
		t.Fatalf("cleanup room_users: %v", err)
	}
	if err := db.Exec("DELETE FROM rooms WHERE id = ?", roomID).Error; err != nil {
		t.Fatalf("cleanup rooms: %v", err)
	}
}

func countActiveRoomUsers(t *testing.T, db *gorm.DB, roomID string) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&RoomUser{}).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Count(&count).Error; err != nil {
		t.Fatalf("count room users: %v", err)
	}
	return count
}

func TestUserJoinRoomConcurrentCreatesAreIdempotent(t *testing.T) {
	repo, db := openMySQLTestRepository(t)

	roomID := fmt.Sprintf("room-concurrent-create-%d", time.Now().UnixNano())
	cleanupRoom(t, db, roomID)
	t.Cleanup(func() { cleanupRoom(t, db, roomID) })

	const clients = 8
	start := make(chan struct{})
	errs := make(chan error, clients)

	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- repo.UserJoinRoom(
				context.Background(),
				fmt.Sprintf("user-%02d", i),
				fmt.Sprintf("User %02d", i),
				roomID,
				"node-1",
				1000,
			)
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("join should be idempotent under concurrent room creation: %v", err)
		}
	}

	var rooms int64
	if err := db.Model(&Room{}).Where("id = ?", roomID).Count(&rooms).Error; err != nil {
		t.Fatalf("count rooms: %v", err)
	}
	if rooms != 1 {
		t.Fatalf("rooms = %d, want 1", rooms)
	}
	if got := countActiveRoomUsers(t, db, roomID); got != clients {
		t.Fatalf("active users = %d, want %d", got, clients)
	}
}

func TestUserJoinRoomConcurrentRespectsCapacity(t *testing.T) {
	repo, db := openMySQLTestRepository(t)

	roomID := fmt.Sprintf("room-capacity-%d", time.Now().UnixNano())
	cleanupRoom(t, db, roomID)
	t.Cleanup(func() { cleanupRoom(t, db, roomID) })

	const (
		clients  = 8
		capacity = 3
	)

	start := make(chan struct{})
	var success int32
	var full int32
	errs := make(chan error, clients)

	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := repo.UserJoinRoom(
				context.Background(),
				fmt.Sprintf("capacity-user-%02d", i),
				fmt.Sprintf("Capacity User %02d", i),
				roomID,
				"node-1",
				capacity,
			)
			switch {
			case err == nil:
				atomic.AddInt32(&success, 1)
			case strings.Contains(err.Error(), "房间已满"):
				atomic.AddInt32(&full, 1)
			default:
				errs <- err
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected join error: %v", err)
	}
	if got := atomic.LoadInt32(&success); got != capacity {
		t.Fatalf("successful joins = %d, want %d", got, capacity)
	}
	if got := atomic.LoadInt32(&full); got != clients-capacity {
		t.Fatalf("full errors = %d, want %d", got, clients-capacity)
	}
	if got := countActiveRoomUsers(t, db, roomID); got != capacity {
		t.Fatalf("active users = %d, want %d", got, capacity)
	}
}

func TestUserJoinRoomExistingUserCanRejoinFullRoom(t *testing.T) {
	repo, db := openMySQLTestRepository(t)

	roomID := fmt.Sprintf("room-rejoin-full-%d", time.Now().UnixNano())
	cleanupRoom(t, db, roomID)
	t.Cleanup(func() { cleanupRoom(t, db, roomID) })

	if err := repo.UserJoinRoom(context.Background(), "user-1", "User 1", roomID, "node-1", 1); err != nil {
		t.Fatalf("initial join: %v", err)
	}
	if err := repo.UserJoinRoom(context.Background(), "user-1", "User 1", roomID, "node-2", 1); err != nil {
		t.Fatalf("existing user rejoin should not fail when room is full: %v", err)
	}

	err := repo.UserJoinRoom(context.Background(), "user-2", "User 2", roomID, "node-2", 1)
	if err == nil || !strings.Contains(err.Error(), "房间已满") {
		t.Fatalf("second user join error = %v, want room full", err)
	}
	if got := countActiveRoomUsers(t, db, roomID); got != 1 {
		t.Fatalf("active users = %d, want 1", got)
	}
}
