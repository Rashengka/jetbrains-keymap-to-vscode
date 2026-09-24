package layout

import "testing"

func TestGetAndFind(t *testing.T) {
	for _, name := range Names() {
		if _, err := Get(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	qwerty, _ := Get("cz-qwerty")
	qwertz, _ := Get("com.apple.keylayout.Czech")
	if k, ok := qwerty.Find('ě'); !ok || k != (Key{Code: "Digit2"}) {
		t.Errorf("ě: %+v %v", k, ok)
	}
	if k, ok := qwerty.Find('2'); !ok || k != (Key{Code: "Digit2", Shift: true}) {
		t.Errorf("2 needs Shift on the Czech layout: %+v %v", k, ok)
	}
	if k, _ := qwerty.Find('z'); k.Code != "KeyZ" {
		t.Errorf("qwerty z: %+v", k)
	}
	if k, _ := qwertz.Find('z'); k.Code != "KeyY" {
		t.Errorf("qwertz z: %+v", k)
	}
	if _, ok := qwerty.Find('ß'); ok {
		t.Error("ß is not on the Czech layout")
	}
	if _, err := Get("klingon"); err == nil {
		t.Error("unknown layout accepted")
	}
}
