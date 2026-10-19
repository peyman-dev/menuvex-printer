package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sinaqaffari/coffish/desktop/internal/printer"
)

func TestPrintersMigrationInfersLegacyConnectionsIdempotently(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	networkID, err := s.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر قدیمی شبکه", "connection": "192.168.1.90:9100", "paper_width": "58mm",
	})
	if err != nil {
		t.Fatal(err)
	}
	queueID, err := s.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "صف قدیمی", "connection": "", "queue": "POS-80",
	})
	if err != nil {
		t.Fatal(err)
	}
	unknownID, err := s.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "دستگاه بی‌نوع", "connection": "USB",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.MigratePrinterSchema(ctx); err != nil {
		t.Fatal(err)
	}
	net, err := s.Printer(ctx, networkID)
	if err != nil {
		t.Fatal(err)
	}
	if net.Kind != printer.KindNetwork || net.Host != "192.168.1.90" || net.Port != 9100 {
		t.Fatalf("network migration = %+v", net)
	}
	if net.PaperMM != printer.Paper58 || net.WidthDots != 384 || !net.Cut || !net.Active {
		t.Fatalf("defaults after migration = %+v", net)
	}
	queue, _ := s.Printer(ctx, queueID)
	if queue.Kind != printer.KindSpooler || queue.QueueName != "POS-80" {
		t.Fatalf("queue migration = %+v", queue)
	}
	unknown, _ := s.Printer(ctx, unknownID)
	if unknown.Kind != "" {
		t.Fatalf("unknown legacy connection must stay legacy, got %+v", unknown)
	}

	v1, _ := s.Entity(ctx, "printer", networkID)
	if err := s.MigratePrinterSchema(ctx); err != nil {
		t.Fatal(err)
	}
	v2, _ := s.Entity(ctx, "printer", networkID)
	if v1.Version != v2.Version {
		t.Fatalf("migration is not idempotent: version %d -> %d", v1.Version, v2.Version)
	}
}

func TestSavePrinterRejectsDuplicateDestinations(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	base := printer.Profile{
		Name: "چاپگر اول", Kind: printer.KindNetwork, Host: "192.168.1.10", Port: 9100,
		PaperMM: printer.Paper80, WidthDots: 576, Copies: 1, Cut: true, Active: true,
	}
	if _, err := s.SavePrinter(ctx, base); err != nil {
		t.Fatal(err)
	}
	base.Name = "چاپگر دوم"
	if _, err := s.SavePrinter(ctx, base); err == nil {
		t.Fatal("duplicate network destination accepted")
	}
	base.Kind = printer.KindSpooler
	base.QueueName = "POS-80"
	base.Host, base.Port = "", 0
	if _, err := s.SavePrinter(ctx, base); err != nil {
		t.Fatal(err)
	}
	base.Name = "صف تکراری"
	if _, err := s.SavePrinter(ctx, base); err == nil {
		t.Fatal("duplicate queue accepted")
	}
}

func TestRecoverInterruptedPrintJobsMarksUncertainAndBlocksRetry(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id, err := s.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر بازیابی", "kind": "network", "host": "192.168.1.20", "port": "9100",
		"paper_width": "80mm", "is_active": true, "cut": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.EnqueueTestPrintJob(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimPrintJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.RecoverInterruptedPrintJobs(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	got, err := s.PrintJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != PrintStatusFailed || !got.Uncertain || got.Retryable() {
		t.Fatalf("recovered job = %+v", got)
	}
	if got.LastError == "" {
		t.Fatal("recovery must explain the uncertainty")
	}
	if n, err := s.RecoverInterruptedPrintJobs(ctx); err != nil || n != 0 {
		t.Fatalf("recovery is not idempotent: %d %v", n, err)
	}
}
