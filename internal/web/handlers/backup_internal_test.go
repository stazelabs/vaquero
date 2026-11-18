package handlers

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
)

func TestBackupFailMessage(t *testing.T) {
	low := backupFailMessage(fmt.Errorf("snapshot: %w", appdb.ErrLowSpace), "/data/backups")
	if low != "Not enough free disk space to save a backup, so nothing was deleted." {
		t.Fatalf("%q", low)
	}
	other := backupFailMessage(errors.New("permission denied"), "/data/backups")
	if !strings.Contains(other, "/data/backups") || !strings.Contains(other, "nothing was deleted") {
		t.Fatalf("%q", other)
	}
}
