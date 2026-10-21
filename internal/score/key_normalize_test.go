package score

import "testing"

func TestKeyAliasesDoNotSilentlyFallBack(t *testing.T) {
	for _, value := range []string{"D minor", "D 小调", "Dm", "dmin", "D 羽调式（D minor）"} {
		key, err := NormalizeKey(value)
		if err != nil || key != "Dm" {
			t.Fatal(value, key, err)
		}
	}
	for _, value := range []string{"D 羽调", "unknown", "C dorian"} {
		if _, err := NormalizeKey(value); err == nil {
			t.Fatal("不得擅自转换", value)
		}
	}
}

func TestWordlessPhraseKeepsVoiceOrderAndExactNotes(t *testing.T) {
	text := "X:1\nT:\nM:4/4\nL:1/32\nQ:1/4=80\n" + abcVoiceHeader[0] + "\n" + abcVoiceHeader[1] + "\nK:Dm\n% verse\nV: Vocal\nZ|\nV: Ins\nD32|\n"
	before, e := ParseABC(text)
	if e != nil {
		t.Fatal(e)
	}
	vocal, e := PhraseVocalABC(text)
	if e != nil {
		t.Fatal(e)
	}
	after, e := ParseABC(vocal)
	if e != nil {
		t.Fatal(e)
	}
	if len(after.Ins().Notes) != 0 || len(after.Vocal().Notes) != 1 || after.Vocal().Notes[0] != before.Ins().Notes[0] {
		t.Fatal(vocal)
	}
}
