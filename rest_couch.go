//  Copyright (c) 2013 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the
//  License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing,
//  software distributed under the License is distributed on an "AS
//  IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
//  express or implied. See the License for the specific language
//  governing permissions and limitations under the License.

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/couchbaselabs/walrus"
	"github.com/dustin/gomemcached"
	"github.com/gorilla/mux"
	"github.com/robertkrimen/otto"
)

func restCouchServe(rest string, staticPath string) {
	r := mux.NewRouter()
	restCouchAPI(r)
	log.Printf("listening rest-couch on: %v", rest)
	log.Fatal(http.ListenAndServe(rest, authenticationFilter{r}))
}

const maxViewErrors = 100

func referencesVBucket(r *http.Request, rm *mux.RouteMatch) bool {
	return strings.Contains(strings.ToLower(r.RequestURI), "%2f")
}

func doesNotReferenceVBucket(r *http.Request, rm *mux.RouteMatch) bool {
	return !strings.Contains(strings.ToLower(r.RequestURI), "%2f")
}

func includesBucketUUID(r *http.Request, rm *mux.RouteMatch) bool {
	return strings.Contains(strings.ToLower(r.RequestURI), "%3b")
}

func restCouchAPI(r *mux.Router) *mux.Router {
	r.Handle("/{db};{bucketUUID}",
		http.HandlerFunc(couchDbGetDb)).Methods("GET", "HEAD").
		MatcherFunc(includesBucketUUID)
	r.Handle("/{db}",
		http.HandlerFunc(couchDbGetDb)).Methods("GET", "HEAD")

	dbr := r.PathPrefix("/{db}/").Subrouter()

	dbr.Handle("/",
		http.HandlerFunc(couchDbGetDb)).
		Methods("GET", "HEAD")

	dbr.Handle("/_all_docs",
		http.HandlerFunc(deadlinedHandler(time.Second, couchDbAllDocs))).
		Methods("GET")

	dbr.Handle("/_design/{docId}/_view/{viewId}",
		http.HandlerFunc(deadlinedHandler(time.Second, couchDbGetView))).
		Methods("GET")

	dbr.Handle("/_design/{docId}",
		http.HandlerFunc(couchDbGetDesignDoc)).Methods("GET", "HEAD")
	dbr.Handle("/_design/{docId}",
		http.HandlerFunc(couchDbPutDesignDoc)).Methods("PUT")
	dbr.Handle("/_design/{docId}",
		http.HandlerFunc(couchDbDelDesignDoc)).Methods("DELETE")

	dbr.Handle("/{docId}",
		http.HandlerFunc(couchDbGetDoc)).Methods("GET", "HEAD").
		MatcherFunc(doesNotReferenceVBucket)
	dbr.Handle("/{docId}",
		http.HandlerFunc(couchDbPutDoc)).Methods("PUT").
		MatcherFunc(doesNotReferenceVBucket)
	dbr.Handle("/{docId}",
		http.HandlerFunc(couchDbDelDoc)).Methods("DELETE").
		MatcherFunc(doesNotReferenceVBucket)

	dbr.Handle("/{vbucket};{bucketUUID}",
		http.HandlerFunc(couchDbGetDb)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket).
		MatcherFunc(includesBucketUUID)
	dbr.Handle("/{vbucket}",
		http.HandlerFunc(couchDbGetDb)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket)

	dbr.Handle("/{vbucket};{bucketUUID}/{docId}",
		http.HandlerFunc(couchDbGetDoc)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket).
		MatcherFunc(includesBucketUUID)
	dbr.Handle("/{vbucket}/{docId}",
		http.HandlerFunc(couchDbGetDoc)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket)

	dbr.Handle("/{vbucket};{bucketUUID}/_local/{docId}",
		http.HandlerFunc(couchDbGetDoc)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket).
		MatcherFunc(includesBucketUUID)
	dbr.Handle("/{vbucket}/_local/{docId}",
		http.HandlerFunc(couchDbGetDoc)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket)
	dbr.Handle("/{vbucket};{bucketUUID}/_local/{docId}/{source}/{destination}",
		http.HandlerFunc(couchDbGetDoc)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket).
		MatcherFunc(includesBucketUUID)
	dbr.Handle("/{vbucket}/_local/{docId}/{source}/{destination}",
		http.HandlerFunc(couchDbGetDoc)).Methods("GET", "HEAD").
		MatcherFunc(referencesVBucket)

	dbr.Handle("/{vbucket};{bucketUUID}/_revs_diff",
		http.HandlerFunc(couchDbRevsDiff)).Methods("POST").
		MatcherFunc(referencesVBucket).
		MatcherFunc(includesBucketUUID)
	dbr.Handle("/{vbucket}/_revs_diff",
		http.HandlerFunc(couchDbRevsDiff)).Methods("POST").
		MatcherFunc(referencesVBucket)

	dbr.Handle("/{vbucket};{bucketUUID}/_bulk_docs",
		http.HandlerFunc(couchDbBulkDocs)).Methods("POST").
		MatcherFunc(referencesVBucket).
		MatcherFunc(includesBucketUUID)
	dbr.Handle("/{vbucket}/_bulk_docs",
		http.HandlerFunc(couchDbBulkDocs)).Methods("POST").
		MatcherFunc(referencesVBucket)

	dbr.Handle("/{vbucket};{bucketUUID}/_ensure_full_commit",
		http.HandlerFunc(couchDbEnsureFullCommit)).Methods("POST").
		MatcherFunc(referencesVBucket).
		MatcherFunc(includesBucketUUID)
	dbr.Handle("/{vbucket}/_ensure_full_commit",
		http.HandlerFunc(couchDbEnsureFullCommit)).Methods("POST").
		MatcherFunc(referencesVBucket)

	return dbr
}

func couchDbGetDesignDoc(w http.ResponseWriter, r *http.Request) {
	_, _, bucket, ddocId := checkDocId(w, r)
	if bucket == nil || ddocId == "" {
		return
	}
	ddocIdFull := "_design/" + ddocId
	body, err := bucket.GetDDoc(ddocIdFull)
	if err != nil {
		http.Error(w, fmt.Sprintf("getDDoc err: %v, ddocIdFull: %v",
			err, ddocIdFull), 500)
		return
	}
	if body == nil {
		http.Error(w, "Not Found", 404)
		return
	}
	w.Header().Add("X-Couchbase-Meta", walrus.MakeMeta(ddocIdFull))
	w.Write(body)
}

func couchDbPutDesignDoc(w http.ResponseWriter, r *http.Request) {
	_, _, bucket, ddocId := checkDocId(w, r)
	if bucket == nil || ddocId == "" {
		return
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Bad Request, err: %v", err), 400)
		return
	}
	var into map[string]interface{}
	if err = json.Unmarshal(body, &into); err != nil {
		http.Error(w, fmt.Sprintf("Bad Request, err: %v", err), 400)
		return
	}
	if _, err = walrus.CheckDDoc(into); err != nil {
		http.Error(w, fmt.Sprintf("Bad Request, err: %v", err), 400)
		return
	}
	if err = bucket.SetDDoc("_design/"+ddocId, body); err != nil {
		http.Error(w, fmt.Sprintf("Internal Server Error, err: %v", err), 500)
		return
	}
	w.WriteHeader(201)
}

func couchDbDelDesignDoc(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "unimplemented", 501)
}

func couchDbGetDb(w http.ResponseWriter, r *http.Request) {
	_, bucketName, bucket := checkDb(w, r)
	if bucket == nil {
		return
	}
	if r.Method == "HEAD" {
		return
	}
	jsonEncode(w, map[string]interface{}{
		"db_name": bucketName,
	})
}

// TODO this implementation does no conflict resolution
// only suitable for one way replications
func couchDbRevsDiff(w http.ResponseWriter, r *http.Request) {
	_, _, bucket := checkDb(w, r)
	if bucket == nil {
		return
	}
	revsDiffRequest := map[string]interface{}{}
	d := json.NewDecoder(r.Body)
	err := d.Decode(&revsDiffRequest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to parse _revs_diff body as JSON: %v", err), 500)
		return
	}

	revsDiffResponse := map[string]interface{}{}
	for key, val := range revsDiffRequest {
		revsDiffResponse[key] = map[string]interface{}{"missing": val}
	}
	jsonEncode(w, revsDiffResponse)
}

type BulkDocsItemMeta struct {
	Id         string  `json:"id"`
	Rev        string  `json:"rev"`
	Expiration float64 `json:"expiration"`
	Flags      float64 `json:"flags"`
	Deleted    bool    `json:"deleted,omitempty"`
	Att_reason string  `json:"att_reason,omitempty"`
}

type BulkDocsItem struct {
	Meta   BulkDocsItemMeta `json:"meta"`
	Base64 string           `json:"base64"`
}

type BulkDocsRequest struct {
	Docs []BulkDocsItem `json:"docs"`
}

// TODO this implementation uses the wrong memcached commands
// it should use commands that can overwrite the metadata
// for now this is done with the wrong commands just to get
// data transfer up and running
func couchDbBulkDocs(w http.ResponseWriter, r *http.Request) {
	_, _, bucket := checkDb(w, r)
	if bucket == nil {
		return
	}

	var bulkDocsRequest BulkDocsRequest
	d := json.NewDecoder(r.Body)
	err := d.Decode(&bulkDocsRequest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to parse _bulk_docs body as JSON: %v", err), 500)
		return
	}

	bulkDocsResponse := make([]map[string]interface{}, 0, len(bulkDocsRequest.Docs))
	for _, doc := range bulkDocsRequest.Docs {
		key := []byte(doc.Meta.Id)
		vbucketId := VBucketIdForKey(key, bucket.GetBucketSettings().NumPartitions)
		vbucket, _ := bucket.GetVBucket(vbucketId)
		if vbucket == nil {
			http.Error(w, fmt.Sprintf("Invalid vbucket for this key: %v - %v", key, err), 500)
			return
		}

		r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(doc.Base64))
		val, err := ioutil.ReadAll(r)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error decoding base64 data _bulk_docs body as JSON for key: %v - %v", key, err), 500)
			return
		}

		response := vbucket.Dispatch(nil, &gomemcached.MCRequest{
			Opcode:  gomemcached.SET,
			VBucket: vbucketId,
			Key:     key,
			Body:    val,
		})

		// TODO proper error handling
		// for now we just bail if anything ever goes wrong
		if response.Status != gomemcached.SUCCESS {
			log.Printf("Got error writing data: %v - %v", string(key), string(response.Body))
			http.Error(w, "Internal Error", 500)
			return
		} else {
			// TODO return actual revision created
			// here we just lie and pretend to have created the requested revsion (we did not)
			bulkDocsResponse = append(bulkDocsResponse, map[string]interface{}{"id": doc.Meta.Id, "rev": doc.Meta.Rev})
		}
	}
	w.WriteHeader(201)
	jsonEncode(w, bulkDocsResponse)
}

// TODO wait for writes to this vbucket to be persisted
// for now, lie
func couchDbEnsureFullCommit(w http.ResponseWriter, r *http.Request) {
	_, _, bucket := checkDb(w, r)
	if bucket == nil {
		return
	}

	w.WriteHeader(201)
	jsonEncode(w, map[string]interface{}{"ok": true})
}

func couchDbGetDoc(w http.ResponseWriter, r *http.Request) {
	_, _, bucket, docId := checkDocId(w, r)
	if bucket == nil || docId == "" {
		return
	}
	res := GetItem(bucket, []byte(docId), VBActive)
	if res == nil || res.Status != gomemcached.SUCCESS {
		http.Error(w, `{"error": "not_found", "reason": "missing"}`, 404)
		return
	}
	// TODO: Content Type, Accepts, much to leverage from sync_gateway.
	w.Header().Add("X-Couchbase-Meta", walrus.MakeMeta(docId))
	w.Write(res.Body)
}

func couchDbPutDoc(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "unimplemented", 501)
}

func couchDbDelDoc(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "unimplemented", 501)
}

func deadlinedHandler(deadline time.Duration, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		q := ""
		if r.URL.RawQuery != "" {
			q = "?" + r.URL.RawQuery
		}

		wd := time.AfterFunc(deadline, func() {
			log.Printf("%v:%v%v is taking longer than %v",
				r.Method, r.URL.Path, q, deadline)
		})

		h(w, r)

		if !wd.Stop() {
			log.Printf("%v:%v%v eventually finished in %v",
				r.Method, r.URL.Path, q, time.Since(start))
		}
	}
}

func couchDbGetView(w http.ResponseWriter, r *http.Request) {
	p, err := ParseViewParams(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("view param parsing err: %v", err), 400)
		return
	}

	vars, _, bucket, ddocId := checkDocId(w, r)
	if bucket == nil || ddocId == "" {
		return
	}
	viewId, ok := vars["viewId"]
	if !ok || viewId == "" {
		http.Error(w, "missing viewId from path", 400)
		return
	}
	ddocs := bucket.GetDDocs()
	if ddocs == nil {
		http.Error(w, "getDDocs nil", 500)
		return
	}
	ddoc, ok := (*ddocs)["_design/"+ddocId]
	if !ok {
		http.Error(w, "design doc not found", 404)
		return
	}
	view, ok := ddoc.Views[viewId]
	if !ok {
		http.Error(w, "view not found", 404)
		return
	}
	if view.Map == "" {
		http.Error(w, "view map function missing", 400)
		return
	}

	o := otto.New()
	fnv, err := OttoNewFunction(o, view.Map)
	if err != nil {
		http.Error(w, fmt.Sprintf("view map function error: %v", err), 400)
		return
	}

	emits := []*ViewRow{}
	var emitErr error

	o.Set("emit", func(call otto.FunctionCall) otto.Value {
		var key, value interface{}
		var err error

		if len(call.ArgumentList) <= 0 {
			emitErr = fmt.Errorf("emit() invoked with no parameters")
			return otto.UndefinedValue()
		}
		key, err = call.ArgumentList[0].Export()
		if err != nil {
			emitErr = err
			return otto.UndefinedValue()
		}

		if len(call.ArgumentList) >= 2 {
			value, err = call.ArgumentList[1].Export()
			if err != nil {
				emitErr = err
				return otto.UndefinedValue()
			}
		}

		emits = append(emits, &ViewRow{Key: key, Value: value})
		return otto.UndefinedValue()
	})

	vr := &ViewResult{Rows: make([]*ViewRow, 0, 100)}
	np := bucket.GetBucketSettings().NumPartitions
	for vbid := 0; vbid < np; vbid++ {
		vb, _ := bucket.GetVBucket(uint16(vbid))
		if vb != nil {
			var errVisit error
			numErr := 0
			err = vb.Visit(nil, func(key []byte, data []byte) bool {
				docId := string(key)

				if numErr > maxViewErrors {
					return false
				}

				docType := "json"
				var doc interface{}
				err := json.Unmarshal(data, &doc)
				if err != nil {
					doc = base64.StdEncoding.EncodeToString(data)
					docType = "base64"
				}

				odoc, err := OttoFromGo(o, doc)
				if err != nil {
					log.Printf("Error sending object into otto %s: %v",
						data, err)
					errVisit = err
					return false
				}

				meta := map[string]interface{}{
					"id":   docId,
					"type": docType,
				}
				ometa, err := OttoFromGo(o, meta)
				if err != nil {
					log.Printf("Error sending meta object into otto: %v -> %v",
						meta, err)
					errVisit = err
					return false
				}

				_, err = fnv.Call(fnv, odoc, ometa)
				if err != nil {
					log.Printf("Error executing view function on %s (%s) -> %v",
						key, data, err)
					numErr++
					errVisit = err
					return true
				}

				for _, emit := range emits {
					emit.Id = docId
				}

				vr.Rows = append(vr.Rows, emits...)
				emits = emits[:0]
				return true
			})
			if err != nil {
				http.Error(w, fmt.Sprintf("view visit error: %v",
					err), 400)
				return
			}
			if numErr > maxViewErrors && errVisit != nil {
				http.Error(w, fmt.Sprintf("view visit function error: %v",
					errVisit), 400)
				return
			}
		}
	}
	sort.Sort(vr.Rows)

	// TODO: Handle p.Keys.

	vr, err = processViewResult(bucket, vr, p)
	if err != nil {
		http.Error(w, fmt.Sprintf("processViewResult error: %v", err), 400)
		return
	}
	if view.Reduce == "" || p.Reduce == false {
		// TODO: Handle p.UpdateSeq.
		if p.IncludeDocs {
			vr, err = docifyViewResult(bucket, vr)
			if err != nil {
				http.Error(w, fmt.Sprintf("docifyViewResults error: %v", err), 500)
				return
			}
		}
	} else {
		vr, err = reduceViewResult(bucket, vr, p, view.Reduce)
		if err != nil {
			http.Error(w, fmt.Sprintf("reduceViewResult error: %v", err), 400)
			return
		}
	}
	skip := int(p.Skip)
	if skip > 0 {
		if skip > len(vr.Rows) {
			skip = len(vr.Rows)
		}
		vr.Rows = vr.Rows[skip:]
	}
	limit := int(p.Limit)
	if limit > 0 {
		if limit > len(vr.Rows) {
			limit = len(vr.Rows)
		}
		vr.Rows = vr.Rows[:limit]
	}
	vr.TotalRows = len(vr.Rows)

	jsonEncode(w, vr)
}

func checkDb(w http.ResponseWriter, r *http.Request) (
	vars map[string]string, bucketName string, bucket Bucket) {

	vars = mux.Vars(r)
	bucketName, ok := vars["db"]
	if !ok {
		http.Error(w, "missing db parameter", 400)
		return vars, "", nil
	}

	if !currentUser(r).canAccess(bucketName) {
		http.Error(w, "Access denied", 403)
		return vars, "", nil
	}

	bucket = buckets.Get(bucketName)
	if bucket == nil {
		http.Error(w, fmt.Sprintf("no db: %v", bucketName), 404)
		return vars, bucketName, nil
	}

	bucketUUID, ok := vars["bucketUUID"]
	if ok {
		// if it contains a bucket UUID, it MUST match
		actualBucketUUID := bucket.GetBucketSettings().UUID
		if bucketUUID != actualBucketUUID {
			http.Error(w, fmt.Sprintf("uuids_dont_match"), 404)
			return vars, bucketName, nil
		}
	}

	vbucketString, ok := vars["vbucket"]
	if ok {
		// if the request contains a vbucket specification
		// ensure that it refers to a valid vbucket
		// we don't return it because none of our functionality will use it
		bucketName = bucketName + "%2f" + vbucketString
		if vbucketString == "master" {
			return vars, bucketName, bucket
		}
		vbucketIdFull, err := strconv.ParseUint(vbucketString, 10, 16)
		if err != nil {
			http.Error(w, fmt.Sprintf("no db: %v", bucketName), 404)
			return vars, bucketName, nil
		}
		vbucketId := uint16(vbucketIdFull)
		vbucket, _ := bucket.GetVBucket(vbucketId)
		if vbucket == nil {
			http.Error(w, fmt.Sprintf("no db: %v", bucketName), 404)
			return vars, bucketName, nil
		}
	}
	return vars, bucketName, bucket
}

func checkDocId(w http.ResponseWriter, r *http.Request) (
	vars map[string]string, bucketName string, bucket Bucket, docId string) {
	vars, bucketName, bucket = checkDb(w, r)
	if bucket == nil {
		return vars, bucketName, bucket, ""
	}
	docId, ok := vars["docId"]
	if !ok {
		http.Error(w, "missing docId from path", 400)
		return vars, bucketName, bucket, ""
	}
	return vars, bucketName, bucket, docId
}

// Originally from github.com/couchbaselabs/walrus, but modified to
// use ViewParams.

func processViewResult(bucket Bucket, result *ViewResult,
	p *ViewParams) (*ViewResult, error) {
	if p.Key != nil {
		p.StartKey = p.Key
		p.EndKey = p.Key
	}

	if p.StartKey != nil {
		i := sort.Search(len(result.Rows), func(i int) bool {
			return walrus.CollateJSON(result.Rows[i].Key, p.StartKey) >= 0
		})
		if p.Descending {
			result.Rows = result.Rows[:i+1]
		} else {
			result.Rows = result.Rows[i:]
		}
	}

	if p.EndKey != nil {
		i := sort.Search(len(result.Rows), func(i int) bool {
			if p.InclusiveEnd {
				return walrus.CollateJSON(result.Rows[i].Key, p.EndKey) > 0
			}
			return walrus.CollateJSON(result.Rows[i].Key, p.EndKey) >= 0
		})
		if p.Descending {
			result.Rows = result.Rows[i:]
		} else {
			result.Rows = result.Rows[:i]
		}
	}

	if p.Descending {
		reverseViewRows(result.Rows)
	}

	return result, nil
}

func reduceViewResult(bucket Bucket, result *ViewResult,
	p *ViewParams, reduceFunction string) (*ViewResult, error) {
	groupLevel := 0
	if p.Group {
		groupLevel = 0x7fffffff
	}
	if p.GroupLevel > 0 {
		groupLevel = int(p.GroupLevel)
	}

	o := newReducer()
	fnv, err := OttoNewFunction(o, reduceFunction)
	if err != nil {
		return result, err
	}

	initialCapacity := 200

	results := make([]*ViewRow, 0, initialCapacity)
	groupKeys := make([]interface{}, 0, initialCapacity)
	groupValues := make([]interface{}, 0, initialCapacity)

	i := 0
	j := 0

	for i < len(result.Rows) {
		groupKeys = groupKeys[:0]
		groupValues = groupValues[:0]

		startRow := result.Rows[i]
		groupKey := ArrayPrefix(startRow.Key, groupLevel)

		for j = i; j < len(result.Rows); j++ {
			row := result.Rows[j]
			rowKey := ArrayPrefix(row.Key, groupLevel)
			if walrus.CollateJSON(groupKey, rowKey) < 0 {
				break
			}
			groupKeys = append(groupKeys, row.Key)
			groupValues = append(groupValues, row.Value)
		}
		i = j

		okeys, err := OttoFromGoArray(o, groupKeys)
		if err != nil {
			return result, err
		}
		ovalues, err := OttoFromGoArray(o, groupValues)
		if err != nil {
			return result, err
		}

		ores, err := fnv.Call(fnv, okeys, ovalues, otto.FalseValue())
		if err != nil {
			return result, fmt.Errorf("call reduce err: %v, reduceFunction: %v, %v, %v",
				err, reduceFunction, okeys, ovalues)
		}
		gres, err := ores.Export()
		if err != nil {
			return result, fmt.Errorf("converting reduce result err: %v", err)
		}

		results = append(results, &ViewRow{Key: groupKey, Value: gres})
	}

	result.Rows = results
	return result, nil
}

func reverseViewRows(r ViewRows) {
	num := len(r)
	mid := num / 2
	for i := 0; i < mid; i++ {
		r[i], r[num-i-1] = r[num-i-1], r[i]
	}
}

func docifyViewResult(bucket Bucket, result *ViewResult) (
	*ViewResult, error) {
	for _, row := range result.Rows {
		if row.Id != "" {
			res := GetItem(bucket, []byte(row.Id), VBActive)
			if res.Status == gomemcached.SUCCESS {
				var parsedDoc interface{}
				err := json.Unmarshal(res.Body, &parsedDoc)
				if err == nil {
					row.Doc = &ViewDocValue{
						Meta: map[string]interface{}{
							"id":  row.Id,
							"rev": "0",
						},
						Json: parsedDoc,
					}
				} else {
					// TODO: Is this the right encoding for non-json?
					// no
					// row.Doc = Bytes(res.Body)
				}
			} // TODO: Handle else-case when no doc.
		}
	}
	return result, nil
}

func visitVBucketAllDocs(vb *VBucket, ch chan *ViewRow) {
	if vb != nil {
		vb.Visit(nil, func(key []byte, data []byte) bool {
			docId := string(key)
			docType := "json"
			var doc interface{}
			err := json.Unmarshal(data, &doc)
			if err != nil {
				doc = base64.StdEncoding.EncodeToString(data)
				docType = "base64"
			}
			// TODO: The couchdb spec emits Value instead of Doc.
			ch <- &ViewRow{
				Id:  docId,
				Key: docId,
				Doc: &ViewDocValue{
					Meta: map[string]interface{}{
						"id":   docId,
						"type": docType,
						// TODO: rev.
					},
					Json: doc,
				},
			}
			return true
		})
	}
	close(ch)
}

func couchDbAllDocs(w http.ResponseWriter, r *http.Request) {
	_, _, bucket := checkDb(w, r)
	if bucket == nil {
		return
	}
	_, err := ParseViewParams(r) // TODO: Handle params.
	if err != nil {
		http.Error(w, fmt.Sprintf("param parsing err: %v", err), 400)
		return
	}
	out := make(chan *ViewRow)
	np := bucket.GetBucketSettings().NumPartitions
	in := make([]chan *ViewRow, np)
	for vbid := 0; vbid < np; vbid++ {
		in[vbid] = make(chan *ViewRow)
	}
	go MergeViewRows(in, out)
	for vbid := 0; vbid < np; vbid++ {
		vb, _ := bucket.GetVBucket(uint16(vbid))
		go visitVBucketAllDocs(vb, in[vbid])
	}
	w.Write([]byte(`{"rows":[`))
	i := 0
	for vr := range out {
		j, err := json.Marshal(vr)
		if err == nil {
			if i > 0 {
				w.Write([]byte(",\n"))
			}
			_, err = w.Write(j)
			if err == nil {
				i++
			}
		} // TODO: else, json marshalling and Write error handling.
	}
	w.Write([]byte(fmt.Sprintf(`],"total_rows":%v}`, i)))
}
