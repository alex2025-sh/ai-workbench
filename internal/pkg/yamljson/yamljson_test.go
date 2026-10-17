package yamljson

import "testing"

func TestConvert(t *testing.T) {
	j, err := Convert([]byte("port: yes\nto: n\nok: true\nn: 3\nlist: [on, off]\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"list":["on","off"],"n":3,"ok":true,"port":"yes","to":"n"}`
	if string(j) != want {
		t.Fatalf("got %s", j)
	}
}
