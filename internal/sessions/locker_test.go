package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDBLockerLockUnlock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	locker, err := NewDBLocker(db, DBLockerConfig{
		OwnerID:         "node-1",
		TTL:             time.Minute,
		RefreshInterval: time.Hour,
		AcquireTimeout:  time.Second,
		PollInterval:    10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDBLocker: %v", err)
	}

	mock.ExpectQuery("INSERT INTO session_locks").
		WithArgs("sess-1", "node-1", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow("node-1"))

	if err := locker.Lock(context.Background(), "sess-1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	mock.ExpectExec("DELETE FROM session_locks").
		WithArgs("sess-1", "node-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	locker.Unlock("sess-1")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
