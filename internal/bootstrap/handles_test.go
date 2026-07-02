package bootstrap

import (
	"reflect"
	"testing"
)

func TestResourcesCloseRunsHooksInReverseOrder(t *testing.T) {
	var closed []string

	lifecycle := &resources{}
	lifecycle.addClose(func() { closed = append(closed, "pool") })
	lifecycle.addClose(func() { closed = append(closed, "redis") })

	lifecycle.close()

	if got, want := closed, []string{"redis", "pool"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("closed order = %#v, want %#v", got, want)
	}
}
