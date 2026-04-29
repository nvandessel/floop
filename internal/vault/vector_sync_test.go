//go:build cgo

package vault

import (
	"context"
	"testing"

	"github.com/lancedb/lancedb-go/pkg/lancedb"

	"github.com/nvandessel/floop/internal/vectorindex"
)

const testDims = 4

func TestVectorSyncer_LocalToLocal(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source table with data
	srcDB, err := lancedb.Connect(ctx, srcDir, nil)
	if err != nil {
		t.Fatalf("connect src: %v", err)
	}

	lanceSchema, err := vectorindex.BuildLanceSchema(testDims)
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}

	srcTable, err := srcDB.CreateTable(ctx, behaviorTableName, lanceSchema)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	arrowSchema, vectorType := vectorindex.BuildBehaviorSchema(testDims)

	// Add test rows
	for i, id := range []string{"b1", "b2", "b3"} {
		vec := make([]float32, testDims)
		vec[i] = 1.0
		rec, err := buildRecord(arrowSchema, vectorType, id, vec)
		if err != nil {
			t.Fatalf("build record: %v", err)
		}
		if err := srcTable.Add(ctx, rec, nil); err != nil {
			rec.Release()
			t.Fatalf("add row: %v", err)
		}
		rec.Release()
	}
	srcTable.Close()
	srcDB.Close()

	// Sync src → dst
	syncer := NewVectorSyncer(srcDir, dstDir, nil, testDims)
	result, err := syncer.Push(ctx)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	if result.RowsPushed != 3 {
		t.Errorf("RowsPushed = %d, want 3", result.RowsPushed)
	}

	// Verify dst has 3 rows
	dstCount, err := syncer.RemoteRowCount(ctx)
	if err != nil {
		t.Fatalf("RemoteRowCount: %v", err)
	}
	if dstCount != 3 {
		t.Errorf("remote count = %d, want 3", dstCount)
	}
}

func TestVectorSyncer_Idempotent(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source table with data
	srcDB, err := lancedb.Connect(ctx, srcDir, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	lanceSchema, err := vectorindex.BuildLanceSchema(testDims)
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}
	srcTable, err := srcDB.CreateTable(ctx, behaviorTableName, lanceSchema)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	arrowSchema, vectorType := vectorindex.BuildBehaviorSchema(testDims)
	rec, _ := buildRecord(arrowSchema, vectorType, "b1", []float32{1, 0, 0, 0})
	srcTable.Add(ctx, rec, nil)
	rec.Release()
	srcTable.Close()
	srcDB.Close()

	syncer := NewVectorSyncer(srcDir, dstDir, nil, testDims)

	// Push twice
	syncer.Push(ctx)
	result, err := syncer.Push(ctx)
	if err != nil {
		t.Fatalf("second Push: %v", err)
	}

	// Count should still be 1
	count, _ := syncer.RemoteRowCount(ctx)
	if count != 1 {
		t.Errorf("count after double push = %d, want 1", count)
	}
	_ = result
}

func TestVectorSyncer_EmptySource(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create empty source table
	srcDB, _ := lancedb.Connect(ctx, srcDir, nil)
	lanceSchema, _ := vectorindex.BuildLanceSchema(testDims)
	srcTable, _ := srcDB.CreateTable(ctx, behaviorTableName, lanceSchema)
	srcTable.Close()
	srcDB.Close()

	syncer := NewVectorSyncer(srcDir, dstDir, nil, testDims)
	result, err := syncer.Push(ctx)
	if err != nil {
		t.Fatalf("Push empty: %v", err)
	}
	if result.RowsPushed != 0 {
		t.Errorf("RowsPushed = %d, want 0", result.RowsPushed)
	}
}

func TestVectorSyncer_PullRoundTrip(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	remoteDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source and push to "remote"
	srcDB, _ := lancedb.Connect(ctx, srcDir, nil)
	lanceSchema, _ := vectorindex.BuildLanceSchema(testDims)
	srcTable, _ := srcDB.CreateTable(ctx, behaviorTableName, lanceSchema)

	arrowSchema, vectorType := vectorindex.BuildBehaviorSchema(testDims)
	for i, id := range []string{"b1", "b2"} {
		vec := make([]float32, testDims)
		vec[i] = 1.0
		rec, _ := buildRecord(arrowSchema, vectorType, id, vec)
		srcTable.Add(ctx, rec, nil)
		rec.Release()
	}
	srcTable.Close()
	srcDB.Close()

	pushSyncer := NewVectorSyncer(srcDir, remoteDir, nil, testDims)
	pushSyncer.Push(ctx)

	// Pull from "remote" to dst
	pullSyncer := NewVectorSyncer(dstDir, remoteDir, nil, testDims)
	result, err := pullSyncer.Pull(ctx)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if result.RowsPushed != 2 {
		t.Errorf("RowsPulled = %d, want 2", result.RowsPushed)
	}

	localCount, _ := pullSyncer.LocalRowCount(ctx)
	if localCount != 2 {
		t.Errorf("local count after pull = %d, want 2", localCount)
	}
}
