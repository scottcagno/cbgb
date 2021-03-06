package main

import (
	"io/ioutil"
	"os"

	"testing"
	"time"
)

func TestVBucketHash(t *testing.T) {
	vbid := VBucketIdForKey([]byte("hello"), 1024)
	if vbid != 528 {
		t.Errorf("expected vbid of 528, got: %v", vbid)
	}
}

func TestItemBytesPersists(t *testing.T) {
	testBucketDir, _ := ioutil.TempDir("./tmp", "test")
	defer os.RemoveAll(testBucketDir)

	b0, err := NewBucket(testBucketDir,
		&BucketSettings{
			NumPartitions: MAX_VBUCKETS,
		})
	if err != nil {
		t.Errorf("expected NewBucket to work, got: %v", err)
	}
	defer b0.Close()

	b0_numBytes0 := b0.GetItemBytes()
	if b0_numBytes0 <= 0 {
		t.Errorf("initial item bytes should be non-zero")
	}

	r0 := &reqHandler{currentBucket: b0}
	vb0, _ := b0.CreateVBucket(2)

	b0_numBytes1 := b0.GetItemBytes()
	if b0_numBytes1 != b0_numBytes0 {
		t.Errorf("vbucket creation should not affect item bytes")
	}

	b0.SetVBState(2, VBActive)

	if 0 != vb0.stats.Items {
		t.Errorf("expected to have 0 items initially")
	}
	b0_numBytes2 := b0.GetItemBytes()
	if b0_numBytes2 <= b0_numBytes1 {
		t.Errorf("vbucket state metadata change should increase item bytes")
	}

	testLoadInts(t, r0, 2, 5)
	testExpectInts(t, r0, 2, []int{0, 1, 2, 3, 4}, "initial data load")

	if 5 != vb0.stats.Items {
		t.Errorf("expected to have 5 items")
	}
	b0_numBytes3 := b0.GetItemBytes()
	if b0_numBytes3 <= b0_numBytes2 {
		t.Errorf("data changes should increase item bytes")
	}

	err = b0.Flush()
	if err != nil {
		t.Errorf("expected Flush to work, got: %v", err)
	}

	if 5 != vb0.stats.Items {
		t.Errorf("expected to have 5 items still after flushing")
	}
	b0_numBytes4 := b0.GetItemBytes()
	if b0_numBytes4 != b0_numBytes3 {
		t.Errorf("flush should not change item bytes")
	}

	b1, err := NewBucket(testBucketDir,
		&BucketSettings{
			NumPartitions: MAX_VBUCKETS,
		})
	if err != nil {
		t.Errorf("expected NewBucket to work, got: %v", err)
	}
	defer b1.Close()

	b1.Load()

	vb1, _ := b1.GetVBucket(2)

	if vb0.stats.Items != vb1.stats.Items {
		t.Errorf("expected to have %v items by stats after re-loading, got: %v",
			vb0.stats.Items, vb1.stats.Items)
	}
	if vb0.stats.ItemBytes != vb1.stats.ItemBytes {
		t.Errorf("expected to have %v or more ItemBytes after re-loading, got: %v",
			vb0.stats.ItemBytes, vb1.stats.ItemBytes)
	}
	r1 := &reqHandler{currentBucket: b1}
	testExpectInts(t, r1, 2, []int{0, 1, 2, 3, 4}, "data re-load")

	if b0.GetItemBytes() != b1.GetItemBytes() {
		t.Errorf("expected GetItemBytes() to equal %v, got: %v",
			b0.GetItemBytes(), b1.GetItemBytes())
	}
}

func TestExpirationComputin(t *testing.T) {
	current, err := time.Parse(time.RFC3339, "2013-03-05T18:01:00Z")
	if err != nil {
		t.Fatalf("Couldn't parse absolute time: %v", err)
	}

	tests := map[uint32]uint32{
		0:         0,
		838424824: 838424824,
		300:       1362506760,
	}

	for in, out := range tests {
		got := computeExp(in, func() time.Time { return current })

		if got != out {
			t.Errorf("Expected %v for %v, got %v", out, in, got)
		}
	}
}
