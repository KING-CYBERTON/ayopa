package web

import (
	"reflect"
	"testing"
)

func TestPostBlocks(t *testing.T) {
	got := postBlocks("Intro line one\nline two\n\n\n## Heading \r\nBody para\n\n")
	want := []postBlock{{Text: "Intro line one\nline two"}, {Heading: true, Text: "Heading"}, {Text: "Body para"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("postBlocks = %#v", got)
	}
}

func TestLinkedProductIDs(t *testing.T) {
	got := linkedProductIDs("see /shop/12 and https://a.mall.test/shop/7, again /shop/12 but not /shop/x or /shop/0")
	if !reflect.DeepEqual(got, []int64{12, 7}) {
		t.Errorf("linkedProductIDs = %v", got)
	}
	if readMinutes("") != 1 || readMinutes("a b c") != 1 {
		t.Error("readMinutes should be at least 1")
	}
}
