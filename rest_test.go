package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dustin/go-jsonpointer"
	"github.com/dustin/gomemcached"
	"github.com/gorilla/mux"
)

// Don't do any normal logging while running tests.
func init() {
	log.SetOutput(ioutil.Discard)
	*staticPath = "static"
	*adminUser = ""
	bdir := "tmp"
	if err := os.MkdirAll(bdir, 0777); err != nil {
		panic("Can't make tmp dir")
	}
}

func testSetupBuckets(t *testing.T, numPartitions int) (string, *Buckets) {
	d, _ := ioutil.TempDir("./tmp", "test")
	var err error
	bucketSettings = &BucketSettings{
		NumPartitions: numPartitions,
	}
	buckets, err = NewBuckets(d, bucketSettings)
	if err != nil {
		t.Fatalf("testSetupBuckets failed: %v", err)
	}
	if buckets == nil {
		t.Fatalf("testSetupBuckets had nil buckets")
	}
	return d, buckets
}

func testSetupDefaultBucket(t *testing.T, numPartitions int,
	vbid uint16) (string, *Buckets, Bucket) {
	d, buckets := testSetupBuckets(t, numPartitions)
	bucket, err := buckets.New("default", bucketSettings)
	if err != nil {
		t.Fatalf("testSetupDefaultBucket failed: %v", err)
	}
	_, err = bucket.CreateVBucket(vbid)
	if err != nil {
		t.Fatalf("testSetupDefaultBucket CreateVBucket failed: %v", err)
	}
	err = bucket.SetVBState(vbid, VBActive)
	if err != nil {
		t.Fatalf("testSetupDefaultBucket SetVBState failed: %v", err)
	}
	return d, buckets, bucket
}

func testSetupMux(dir string) *mux.Router {
	mr := mux.NewRouter()
	restAPI(mr)
	restNSAPI(mr)
	restCouchAPI(mr)
	return mr
}

func TestRestAPIBuckets(t *testing.T) {
	d, _ := testSetupBuckets(t, 1)
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "http://127.0.0.1/_api/buckets", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to work, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("POST", "http://127.0.0.1/_api/bucketsRescan", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 303 {
		t.Errorf("expected req to work, got: %#v, %v",
			rr, rr.Body.String())
	}
}

func TestCouchDocGet(t *testing.T) {
	// "hello" hash is 528 with 1024 vbuckets.
	d, _, bucket := testSetupDefaultBucket(t, 1024, uint16(528))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "http://127.0.0.1/default/hello", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	res := SetItem(bucket, []byte("hello"), []byte("world"), VBActive)
	if res == nil || res.Status != gomemcached.SUCCESS {
		t.Errorf("expected SetItem to work, got: %v", res)
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default/hello", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
}

func TestCouchDbGet(t *testing.T) {
	d, _, bucket := testSetupDefaultBucket(t, 1024, uint16(528))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "http://127.0.0.1/dbnotexist", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default%2f203456", nil)
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%2f203456"
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default%2f528", nil)
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%2f528"
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}

	// now create a document, make sure that docuemnt
	// access is not confused with vbucket access

	res := SetItem(bucket, []byte("hello"), []byte("world"), VBActive)
	if res == nil || res.Status != gomemcached.SUCCESS {
		t.Errorf("expected SetItem to work, got: %v", res)
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default/hello", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default%2fhello", nil)
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%2fhello"
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	// now test including the bucket UUID
	bucketUUID := bucket.GetBucketSettings().UUID

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default%3bwronguuid", nil)
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%3bwronguuid"
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default%3b"+bucketUUID, nil)
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%3b" + bucketUUID
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}

	// test vbucket AND bucket UUID
	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default%2f528%3bwronguuid", nil)
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%2f528%3bwronguuid"
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", "http://127.0.0.1/default%2f528%3b"+bucketUUID, nil)
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%2f528%3b" + bucketUUID
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}

}

func TestCouchDbRevsDiff(t *testing.T) {
	d, _, _ := testSetupDefaultBucket(t, 1024, uint16(528))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	revsDiffRequest := map[string]interface{}{
		"doca": "1-0000dac6571554820000000000000000",
	}

	revsDiffRequestJSON, err := json.Marshal(revsDiffRequest)
	if err != nil {
		t.Errorf("Error marshaling JSON: %v", err)
	}

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "http://127.0.0.1/default%2f528/_revs_diff", bytes.NewReader(revsDiffRequestJSON))
	// manually set the RequestURI (not populated in test env)
	r.RequestURI = "/default%2f528/_revs_diff"
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	actualRevsDiffResponse := map[string]interface{}{}
	err = json.Unmarshal(rr.Body.Bytes(), &actualRevsDiffResponse)
	missingMap, ok := actualRevsDiffResponse["doca"]
	if !ok {
		t.Errorf("expected response to contain doca, got: %#v", actualRevsDiffResponse)
	} else {
		if missingMap.(map[string]interface{})["missing"] != "1-0000dac6571554820000000000000000" {
			t.Errorf("expected missing revision to be 1-0000dac6571554820000000000000000, got: %v", missingMap.(map[string]interface{})["missing"])
		}
	}
}

func TestCouchPutDDoc(t *testing.T) {
	testCouchPutDDoc(t, 1)
	testCouchPutDDoc(t, MAX_VBUCKETS)
}

func testCouchPutDDoc(t *testing.T, numPartitions int) {
	d, _, _ := testSetupDefaultBucket(t, numPartitions, uint16(0))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	d0 := []byte(`{
		"_id":"_design/d0",
		"language": "not-javascript",
		"views": {
			"v0": {
				"map": "function(doc) { emit(doc.amount, null) }"
			}
		}
    }`)
	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("PUT",
		"http://127.0.0.1/default/_design/d0",
		bytes.NewBuffer([]byte(d0)))
	mr.ServeHTTP(rr, r)
	if rr.Code != 400 {
		t.Errorf("expected req to 400, got: %#v, %v",
			rr, rr.Body.String())
	}

	d0 = []byte{}
	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("PUT",
		"http://127.0.0.1/default/_design/d0",
		bytes.NewBuffer([]byte(d0)))
	mr.ServeHTTP(rr, r)
	if rr.Code != 400 {
		t.Errorf("expected req to 400, got: %#v, %v",
			rr, rr.Body.String())
	}

	d0 = []byte(`{
		"_id":"_design/d0",
		"views": {
			"v0": {
				"map": "function(doc) { emit(doc.amount, null) }"
			}
		}
    }`)
	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("PUT",
		"http://127.0.0.1/default/_design/d0",
		bytes.NewBuffer([]byte(d0)))
	mr.ServeHTTP(rr, r)
	if rr.Code != 201 {
		t.Errorf("expected req to 201, got: %#v, %v",
			rr, rr.Body.String())
	}
}

func testSetupDDoc(t *testing.T, bucket Bucket, ddoc string,
	docFmt func(i int) string) {
	if docFmt == nil {
		docFmt = func(i int) string {
			return fmt.Sprintf(`{"amount":%d}`, i)
		}
	}

	d0 := []byte(strings.Replace(ddoc, "\n", "", -1))

	err := bucket.SetDDoc("_design/d0", d0)
	if err != nil {
		t.Errorf("expecting SetDDoc to work, got: %v", err)
	}

	dx, err := bucket.GetDDoc("_design/d0")
	if err != nil {
		t.Errorf("expecting GetDDoc to work, got: %v", err)
	}
	if !bytes.Equal(dx, d0) {
		t.Errorf("not the same doc, want: %v, got: %v", d0, dx)
	}

	var res *gomemcached.MCResponse

	res = SetItem(bucket, []byte("a"), []byte(docFmt(1)),
		VBActive)
	if res == nil || res.Status != gomemcached.SUCCESS {
		t.Errorf("expected SetItem to work, got: %v", res)
	}
	res = SetItem(bucket, []byte("b"), []byte(docFmt(3)),
		VBActive)
	if res == nil || res.Status != gomemcached.SUCCESS {
		t.Errorf("expected SetItem to work, got: %v", res)
	}
	res = SetItem(bucket, []byte("c"), []byte(docFmt(4)),
		VBActive)
	if res == nil || res.Status != gomemcached.SUCCESS {
		t.Errorf("expected SetItem to work, got: %v", res)
	}
	res = SetItem(bucket, []byte("d"), []byte(docFmt(2)),
		VBActive)
	if res == nil || res.Status != gomemcached.SUCCESS {
		t.Errorf("expected SetItem to work, got: %v", res)
	}
}

func TestCouchViewBasic(t *testing.T) {
	d, _, bucket := testSetupDefaultBucket(t, 1, uint16(0))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	testSetupDDoc(t, bucket, `{
		"_id":"_design/d0",
		"language": "javascript",
		"views": {
			"v0": {
				"map": "function(doc) { emit(doc.amount, null) }"
			}
		}
    }`, nil)

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "http://127.0.0.1/default/hello", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/not-a-design-doc/_view/v0", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/not-a-view", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 404 {
		t.Errorf("expected req to 404, got: %#v, %v",
			rr, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd := &ViewResult{}
	err := json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k := []string{"a", "d", "b", "c"}
	a := []int{1, 2, 3, 4}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
		if row.Doc != nil {
			t.Errorf("expected no doc since it's not include_docs, got: %#v", row)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?startkey=2", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"d", "b", "c"}
	a = []int{2, 3, 4}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?startkey=2&endkey=3", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"d", "b"}
	a = []int{2, 3}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?startkey=1&endkey=3&key=2", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"d"}
	a = []int{2}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?startkey=3&endkey=1", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	if dd.TotalRows != 0 {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			0, dd.TotalRows, dd, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0"+
			"?startkey=1&endkey=3&inclusive_end=false", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"a", "d"}
	a = []int{1, 2}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?startkey=2&endkey=4&limit=1", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"d"}
	a = []int{2}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?descending=true", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"c", "b", "d", "a"}
	a = []int{4, 3, 2, 1}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"startkey=3&descending=true", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"b", "d", "a"}
	a = []int{3, 2, 1}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"include_docs=true", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k = []string{"a", "d", "b", "c"}
	a = []int{1, 2, 3, 4}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
		if row.Doc == nil {
			t.Errorf("expected include_doc to give a doc, got: %#v", row)
		}
		if int(row.Doc.Json.(map[string]interface{})["amount"].(float64)) != (i + 1) {
			t.Errorf("Expected %v at %v, got %v", i+1, i, row.Doc.Json)
		}
	}
}

func TestCouchViewReduceBasic(t *testing.T) {
	d, _, bucket := testSetupDefaultBucket(t, 1, uint16(0))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	testSetupDDoc(t, bucket, `{
		"_id":"_design/d0",
		"language": "javascript",
		"views": {
			"v0": {
				"map": "function(doc) { emit(doc.amount, 1); }",
				"reduce": "function(keys, values, rereduce) {
                              var sum = 0;
                              for (var i = 0; i < values.length; i++) {
                                sum = sum + values[i];
                              }
                              return sum;
                           }"
			}
		}
    }`, nil)

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd := &ViewResult{}
	err := json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	if dd.TotalRows != 1 {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			1, dd.TotalRows, dd, rr.Body.String())
	}
	exp := 4
	if exp != int(dd.Rows[0].Value.(float64)) {
		t.Errorf("expected row value %#v to match %#v in row %#v",
			dd.Rows[0].Value, exp, dd.Rows[0])
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?reduce=false", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k := []string{"a", "d", "b", "c"}
	a := []int{1, 2, 3, 4}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		if a[i] != int(row.Key.(float64)) {
			t.Errorf("expected row %#v to match a %#v, i %v", row, a[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"reduce=true&startkey=2&endkey=3", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	if dd.TotalRows != 1 {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			1, dd.TotalRows, dd, rr.Body.String())
	}
	exp = 2
	if exp != int(dd.Rows[0].Value.(float64)) {
		t.Errorf("expected row value %#v to match %#v in row %#v",
			dd.Rows[0].Value, exp, dd.Rows[0])
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"reduce=true&startkey=2&endkey=3&skip=100", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	if dd.TotalRows != 0 {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			0, dd.TotalRows, dd, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"reduce=true&descending=true", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	if dd.TotalRows != 1 {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			1, dd.TotalRows, dd, rr.Body.String())
	}
	exp = 4
	if exp != int(dd.Rows[0].Value.(float64)) {
		t.Errorf("expected row value %#v to match %#v in row %#v",
			dd.Rows[0].Value, exp, dd.Rows[0])
	}
}

func TestCouchViewGroupLevel(t *testing.T) {
	d, _, bucket := testSetupDefaultBucket(t, 1, uint16(0))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	testSetupDDoc(t, bucket, `{
		"_id":"_design/d0",
		"language": "javascript",
		"views": {
			"v0": {
				"map": "function(doc) { emit([doc.category, doc.amount], 1); }",
				"reduce": "function(keys, values, rereduce) {
                              var sum = 0;
                              for (var i = 0; i < values.length; i++) {
                                sum = sum + values[i];
                              }
                              return sum;
                           }"
			}
		}
    }`, func(i int) string {
		return fmt.Sprintf(`{"amount":%d,"category":%d}`, i, i/2)
	})

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd := &ViewResult{}
	err := json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	if dd.TotalRows != 1 {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			1, dd.TotalRows, dd, rr.Body.String())
	}
	exp := 4
	if exp != int(dd.Rows[0].Value.(float64)) {
		t.Errorf("expected row value %#v to match %#v in row %#v",
			dd.Rows[0].Value, exp, dd.Rows[0])
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"reduce=false&limit=1000", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k := []string{"a", "d", "b", "c"}
	a := []string{"[0,1]", "[1,2]", "[1,3]", "[2,4]"}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
		j, _ := json.Marshal(row.Key)
		if a[i] != string(j) {
			t.Errorf("expected row %#v to match a %#v, i %v, j %v",
				row, a[i], i, j)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"group_level=1", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	g := []string{"[0]", "[1]", "[2]"}
	v := []int{1, 2, 1}
	if dd.TotalRows != len(g) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(g), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		j, _ := json.Marshal(row.Key)
		if g[i] != string(j) {
			t.Errorf("expected row %#v to match key %#v, i %v, j %v",
				row, a[i], i, j)
		}
		if v[i] != int(row.Value.(float64)) {
			t.Errorf("expected row %#v to match val %#v, i %v",
				row, v[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"group_level=2", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	g = []string{"[0,1]", "[1,2]", "[1,3]", "[2,4]"}
	v = []int{1, 1, 1, 1}
	if dd.TotalRows != len(g) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(g), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		j, _ := json.Marshal(row.Key)
		if g[i] != string(j) {
			t.Errorf("expected row %#v to match key %#v, i %v, j %v",
				row, a[i], i, j)
		}
		if v[i] != int(row.Value.(float64)) {
			t.Errorf("expected row %#v to match val %#v, i %v",
				row, v[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"group_level=2&skip=1&limit=2", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	g = []string{"[1,2]", "[1,3]"}
	v = []int{1, 1}
	if dd.TotalRows != len(g) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(g), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		j, _ := json.Marshal(row.Key)
		if g[i] != string(j) {
			t.Errorf("expected row %#v to match key %#v, i %v, j %v",
				row, a[i], i, j)
		}
		if v[i] != int(row.Value.(float64)) {
			t.Errorf("expected row %#v to match val %#v, i %v",
				row, v[i], i)
		}
	}

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_design/d0/_view/v0?"+
			"group=true", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	g = []string{"[0,1]", "[1,2]", "[1,3]", "[2,4]"}
	v = []int{1, 1, 1, 1}
	if dd.TotalRows != len(g) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(g), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		j, _ := json.Marshal(row.Key)
		if g[i] != string(j) {
			t.Errorf("expected row %#v to match key %#v, i %v, j %v",
				row, a[i], i, j)
		}
		if v[i] != int(row.Value.(float64)) {
			t.Errorf("expected row %#v to match val %#v, i %v",
				row, v[i], i)
		}
	}
}

func TestReverseViewRows(t *testing.T) {
	var r ViewRows
	r = []*ViewRow{}
	reverseViewRows(r)
	if len(r) != 0 {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}

	r = []*ViewRow{
		&ViewRow{Id: "a"},
	}
	reverseViewRows(r)
	if len(r) != 1 {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}
	if r[0].Id != "a" {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}

	r = []*ViewRow{
		&ViewRow{Id: "a"},
		&ViewRow{Id: "b"},
	}
	reverseViewRows(r)
	if len(r) != 2 {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}
	if r[0].Id != "b" || r[1].Id != "a" {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}

	r = []*ViewRow{
		&ViewRow{Id: "a"},
		&ViewRow{Id: "b"},
		&ViewRow{Id: "c"},
	}
	reverseViewRows(r)
	if len(r) != 3 {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}
	if r[0].Id != "c" || r[1].Id != "b" || r[2].Id != "a" {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}

	r = []*ViewRow{
		&ViewRow{Id: "a"},
		&ViewRow{Id: "b"},
		&ViewRow{Id: "c"},
		&ViewRow{Id: "d"},
	}
	reverseViewRows(r)
	if len(r) != 4 {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}
	if r[0].Id != "d" || r[1].Id != "c" || r[2].Id != "b" || r[3].Id != "a" {
		t.Errorf("reversing empty ViewRows should work, got: %#v", r)
	}
}

func TestCouchAllDocs(t *testing.T) {
	d, _, bucket := testSetupDefaultBucket(t, 1, uint16(0))
	defer os.RemoveAll(d)
	mr := testSetupMux(d)

	rr := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "http://127.0.0.1/default/_all_docs", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd := &ViewResult{}
	err := json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	if dd.TotalRows != 0 || len(dd.Rows) != 0 {
		t.Errorf("expected 0 rows, got: %v, %v", dd.TotalRows, len(dd.Rows))
	}

	testSetupDDoc(t, bucket, `{
		"_id":"_design/d0",
		"language": "javascript",
		"views": {
			"v0": {
				"map": "function(doc) { emit(doc.amount, null) }"
			}
		}
    }`, nil)

	rr = httptest.NewRecorder()
	r, _ = http.NewRequest("GET",
		"http://127.0.0.1/default/_all_docs", nil)
	mr.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("expected req to 200, got: %#v, %v",
			rr, rr.Body.String())
	}
	dd = &ViewResult{}
	err = json.Unmarshal(rr.Body.Bytes(), dd)
	if err != nil {
		t.Errorf("expected good view result, got: %v", err)
	}
	k := []string{"a", "b", "c", "d"}
	if dd.TotalRows != len(k) {
		t.Errorf("expected %v rows, got: %v, %v, %v",
			len(k), dd.TotalRows, dd, rr.Body.String())
	}
	for i, row := range dd.Rows {
		if k[i] != row.Id {
			t.Errorf("expected row %#v to match k %#v, i %v", row, k[i], i)
		}
	}
}

func jsonFindParse(t *testing.T, b []byte, path string) (interface{}, error) {
	d, err := jsonpointer.Find(b, path)
	if err != nil {
		return nil, err
	}
	var rv interface{}
	err = json.Unmarshal(d, &rv)
	return rv, err
}

func validateSubset(t *testing.T, upath, exname string, got, exemplar []byte) {
	ptrs, err := jsonpointer.ListPointers(got)
	if err != nil {
		t.Fatalf("Error listing pointers: %v", err)
	}

	for _, p := range ptrs {
		dg, err := jsonFindParse(t, got, p)
		if err != nil {
			t.Fatalf("%v: Error loading %q from %s: %v",
				upath, p, got, err)
		}

		eg, err := jsonFindParse(t, exemplar, p)
		if err != nil {
			t.Errorf("%v: Error loading %q from exemplar %v: %v\n%s",
				upath, p, exname, err, exemplar)
			continue
		}
		dt := fmt.Sprintf("%T", dg)
		et := fmt.Sprintf("%T", eg)
		if dt != et {
			t.Errorf("%v: Type mismatch at %v of %v (%v != %v)",
				upath, p, exname, dt, et)
		}
	}
}

func validateJson(t *testing.T, upath, jsonbody, path string) {
	f, err := os.Open("testdata/" + path + ".json")
	if err != nil {
		t.Fatalf("Error opening exemplar: %v", err)
	}
	defer f.Close()
	exemplar, err := ioutil.ReadAll(f)
	if err != nil {
		t.Fatalf("Error loading exemplar %v: %v", path, err)
	}

	validateSubset(t, upath, path, []byte(jsonbody), exemplar)
}

func TestRestAPIPoolsDefault(t *testing.T) {
	d, _ := testSetupBuckets(t, 1)
	defer os.RemoveAll(d)

	b, err := buckets.New("test", bucketSettings)
	if err != nil {
		t.Fatalf("Error initializing test bucket: %v", err)
	}
	defer b.Close()

	b.SetDDoc("_design/buildboard",
		[]byte(`{"views": {"builds": {"map": "function (doc, meta) {}"}}}`))

	mr := testSetupMux(d)

	ns_server_paths := map[string]string{
		"/pools/default/buckets/{bucketname}/statsDirectory":     "",
		"/pools/default/buckets/{bucketname}/stats":              "",
		"/pools/default/buckets/{bucketname}/nodes":              "",
		"/pools/default/buckets/{bucketname}/nodes/{node}/stats": "",
		"/pools/default/buckets/{bucketname}/ddocs":              "pools_default_buckets_cbfs_ddocs",
		"/pools/default/buckets/{bucketname}/localRandomKey":     "pools_default_buckets_cbfs_localRandomKey",
		"/pools/default/buckets/{bucketname}":                    "pools_default_buckets_cbfs",
		"/pools/default/stats":                                   "",
		"/pools/default/buckets":                                 "pools_default_buckets",
		"/pools/default":                                         "pools_default",
		"/pools":                                                 "pools",
		"/versions":                                              "versions",
	}

	for pattern, fn := range ns_server_paths {
		rr := httptest.NewRecorder()

		// Set vars
		p := strings.Replace(pattern, "{bucketname}", "test", 1)
		p = strings.Replace(p, "{node}", "localhost", 1)

		r, _ := http.NewRequest("GET", "http://127.0.0.1"+p, nil)
		mr.ServeHTTP(rr, r)
		switch {
		case rr.Code == 501 && fn == "":
			t.Logf("%v is not yet implemented", p)
		case rr.Code == 200:
			validateJson(t, p, rr.Body.String(), fn)
		default:
			t.Errorf("expected %v to work, got: %#v, %v",
				p, rr, rr.Body.String())
		}
	}
}
