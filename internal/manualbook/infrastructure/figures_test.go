package infrastructure

import (
	"reflect"
	"strconv"
	"testing"
)

func TestPageSpecBoundsAndOrder(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want []int
	}{
		{"", []int{1, 2, 3}},
		{"3,1-2,2", []int{3, 1, 2}},
		{"0-4", []int{1, 2, 3}},
		{strconv.Itoa(int(^uint(0) >> 1)), nil},
		{"2-1", nil},
		{"invalid", nil},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := parsePageSpec(tc.spec, 3)
			if (err != nil) != (tc.want == nil) {
				t.Fatalf("error = %v, want pages %v", err, tc.want)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pages = %v, want %v", got, tc.want)
			}
		})
	}
}
