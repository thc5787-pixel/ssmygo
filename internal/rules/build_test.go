package rules

import (
	"os"
	"reflect"
	"testing"
	"time"
)

const userText = "||google.com^\n@@||mail.google.com^\n/re/\n"

func TestBuildUserSets(t *testing.T) {
	dir := t.TempDir()
	r, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, userText, dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.UserDirect == "" || r.UserProxied == "" {
		t.Fatalf("user sets not written: %+v", r)
	}
	if !loadSet(t, r.UserDirect).Match("x.mail.google.com") || loadSet(t, r.UserDirect).Match("google.com") {
		t.Error("user direct set wrong")
	}
	if !loadSet(t, r.UserProxied).Match("www.google.com") {
		t.Error("user proxied set wrong")
	}
	if !reflect.DeepEqual(r.UserSkipped, []int{3}) {
		t.Errorf("skipped %v", r.UserSkipped)
	}
	// geosite sets unchanged by user rules
	if !loadSet(t, r.Direct).Match("baidu.com") {
		t.Error("geosite direct set wrong")
	}
}

func TestBuildNoUserRules(t *testing.T) {
	dir := t.TempDir()
	r, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "! only comments\n", dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.UserDirect != "" || r.UserProxied != "" || r.UserSkipped != nil {
		t.Fatalf("got %+v", r)
	}
	// Only proxied rules: only that set.
	r, _ = Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "a.com", dir)
	if r.UserDirect != "" || r.UserProxied == "" {
		t.Fatalf("got %+v", r)
	}
}

// The user text is part of the cache key; changing it does not rebuild the
// (large) geosite sets.
func TestBuildUserCache(t *testing.T) {
	dir := t.TempDir()
	r, _ := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "a.com", dir)
	geo1, _ := os.Stat(r.Direct)
	time.Sleep(20 * time.Millisecond)
	r2, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "b.com\n/re/", dir)
	if err != nil {
		t.Fatal(err)
	}
	if loadSet(t, r2.UserProxied).Match("a.com") || !loadSet(t, r2.UserProxied).Match("b.com") {
		t.Fatal("user set not rebuilt")
	}
	if !reflect.DeepEqual(r2.UserSkipped, []int{2}) {
		t.Fatalf("skipped %v", r2.UserSkipped)
	}
	geo2, _ := os.Stat(r2.Direct)
	if !geo1.ModTime().Equal(geo2.ModTime()) {
		t.Fatal("geosite sets rebuilt for a user-rule change")
	}
	// Same text again: reused, skipped lines still reported.
	u1, _ := os.Stat(r2.UserProxied)
	time.Sleep(20 * time.Millisecond)
	r3, _ := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "b.com\n/re/", dir)
	u2, _ := os.Stat(r3.UserProxied)
	if !u1.ModTime().Equal(u2.ModTime()) || !reflect.DeepEqual(r3.UserSkipped, []int{2}) {
		t.Fatalf("user cache: reused=%v skipped=%v", u1.ModTime().Equal(u2.ModTime()), r3.UserSkipped)
	}
}

// A geosite set with no domains (every group unknown, or no groups) has no
// file and an empty path; the other set still works, also from the cache.
func TestBuildEmptyGeositeSet(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ { // cold, then cached
		r, err := Build(fixture(), []string{"nope"}, []string{"geolocation-!cn"}, "", dir)
		if err != nil {
			t.Fatalf("#%d: %v", i, err)
		}
		if r.Direct != "" || r.Proxied == "" || !reflect.DeepEqual(r.Missing, []string{"nope"}) {
			t.Fatalf("#%d: %+v", i, r)
		}
		if !loadSet(t, r.Proxied).Match("google.com") {
			t.Fatalf("#%d: proxied set wrong", i)
		}
	}
	r, err := Build(fixture(), nil, nil, "", dir)
	if err != nil || r.Direct != "" || r.Proxied != "" {
		t.Fatalf("no groups: %+v %v", r, err)
	}
	// Back to a full set: rebuilt with a file again.
	r, err = Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	if err != nil || r.Direct == "" || !loadSet(t, r.Direct).Match("baidu.com") {
		t.Fatalf("full: %+v %v", r, err)
	}
}

// Final review #3: a geosite build that fails part-way must not leave the
// old stamp, or the next build of the old input is a false cache hit over
// half-written sets.
func TestFailedGeositeBuildDropsStamp(t *testing.T) {
	dir := t.TempDir()
	a, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(a.Direct)
	oldProxied, _ := os.ReadFile(a.Proxied)
	// proxied.gob cannot be written: a directory takes its place.
	os.Remove(a.Proxied)
	os.Mkdir(a.Proxied, 0o755)
	if _, err := Build(fixture(), []string{"geolocation-!cn"}, []string{"cn"}, "", dir); err == nil {
		t.Fatal("build into a directory succeeded")
	}
	// As when the write failed on a locked file: the old proxied set stays.
	os.Remove(a.Proxied)
	os.WriteFile(a.Proxied, oldProxied, 0o644)
	again, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(again.Direct); string(got) != string(want) {
		t.Fatal("direct set left from the failed build (stale stamp hit)")
	}
}

// Final review #2a: a build never deletes a set file, which the running
// core config may still name until the new result is adopted.
func TestBuildKeepsOldSetFiles(t *testing.T) {
	dir := t.TempDir()
	a, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, userText, dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(fixture(), []string{"cn"}, []string{"geolocation-!cn"}, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.UserDirect != "" || b.UserProxied != "" {
		t.Fatalf("new result still names user sets: %+v", b)
	}
	for _, p := range []string{a.UserDirect, a.UserProxied} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed while the running config may still name it", p)
		}
	}
}
