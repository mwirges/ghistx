package ngram

import "testing"

func TestTestingVector(t *testing.T) {
	// Validation vector from C test suite:
	// n-grams of "testing" must be 5 grams summing to 0x22B2525.
	grams := Gen("testing")
	if len(grams) != 5 {
		t.Fatalf("len(Gen(\"testing\")) = %d, want 5", len(grams))
	}
	var sum uint32
	for _, g := range grams {
		sum += g
	}
	const want = uint32(0x22B2525)
	if sum != want {
		t.Errorf("sum of grams = 0x%X, want 0x%X", sum, want)
	}
}

func TestEmpty(t *testing.T) {
	grams := Gen("")
	if len(grams) != 0 {
		t.Errorf("Gen(\"\") = %v, want empty", grams)
	}
}

func TestSingleChar(t *testing.T) {
	grams := Gen("a")
	if len(grams) != 0 {
		t.Errorf("Gen(\"a\") = %v, want empty", grams)
	}
}

func TestTwoChars(t *testing.T) {
	grams := Gen("ab")
	if len(grams) != 0 {
		t.Errorf("Gen(\"ab\") = %v, want empty", grams)
	}
}

func TestThreeChars(t *testing.T) {
	grams := Gen("abc")
	if len(grams) != 1 {
		t.Fatalf("Gen(\"abc\") = %v, want 1 gram", grams)
	}
	// 'a'=0x61, 'b'=0x62, 'c'=0x63 -> 0x616263
	const want = uint32(0x616263)
	if grams[0] != want {
		t.Errorf("gram = 0x%X, want 0x%X", grams[0], want)
	}
}

func TestFourChars(t *testing.T) {
	grams := Gen("abcd")
	if len(grams) != 2 {
		t.Fatalf("Gen(\"abcd\") len = %d, want 2", len(grams))
	}
}
