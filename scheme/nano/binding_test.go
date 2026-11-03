package nano

import (
	"strings"
	"testing"
)

// Vectors made with the Python reference (dhyabi2/dual-rail binding.py over
// nano-wallet-xno ed25519_blake2b.py) from a synthetic seed (bytes 0..31,
// index 7). Ed25519 is deterministic, so Go must verify these exactly.
const (
	vPayer = "nano_3ar3porks6g19gucmephzczmn6iygpu1ehraap87qij7idnef4h6cdhc4oit"
	vSig   = "CBB87DB622B6C2AD82A8A2C714AEAA07097D74019DBCE58930F04FF399B33ABA34FA3AF7B1FA0C3F2AFD9A56FD8E8815958D9D7C41A78297BA315FA6A0AD3D0C"
	vHash  = "ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113"
	vPay   = "nano_1111111111111111111111111111111111111111111111111111hifc8npp"
	vNonce = "6a1b2c3d.0123456789abcdef0123456789abcdef"
	vRes   = "https://api.example.test/v1/answer"
	vOther = "nano_3fao47pnt8sske1m8xun7g4d93jhr9ejwj5w7xwt66i4cq64oq1ttw1ophfe"
)

func TestBindingVectorVerifies(t *testing.T) {
	msg, err := BindingMessage(vHash, vNonce, vRes, vPay)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBinding(vPayer, msg, vSig); err != nil {
		t.Fatalf("reference signature refused: %v", err)
	}
	// xrb_ spelling of the same payee signs the same bytes
	msg2, _ := BindingMessage(vHash, vNonce, vRes, "xrb_"+vPay[5:])
	if string(msg) != string(msg2) {
		t.Fatal("xrb_ and nano_ spellings of pay-to must give the same message")
	}
}

func TestBindingRefusals(t *testing.T) {
	good, _ := BindingMessage(vHash, vNonce, vRes, vPay)
	other := func(h, n, r, p string) []byte { m, _ := BindingMessage(h, n, r, p); return m }
	flip := strings.Replace(vHash, "ECCB", "ECCC", 1)
	cases := []struct {
		name    string
		payer   string
		msg     []byte
		sig     string
		wantErr error
	}{
		{"another account's key", vOther, good, vSig, ErrBindingInvalid},
		{"other block hash", vPayer, other(flip, vNonce, vRes, vPay), vSig, ErrBindingInvalid},
		{"other nonce", vPayer, other(vHash, vNonce+"x", vRes, vPay), vSig, ErrBindingInvalid},
		{"other resource", vPayer, other(vHash, vNonce, vRes+"/b", vPay), vSig, ErrBindingInvalid},
		{"other payee", vPayer, other(vHash, vNonce, vRes, vOther), vSig, ErrBindingInvalid},
		{"short signature", vPayer, good, vSig[:126], ErrBindingMalformed},
		{"not hex", vPayer, good, strings.Repeat("zz", 64), ErrBindingMalformed},
		{"bad payer checksum", vPayer[:len(vPayer)-1] + "3", good, vSig, ErrBindingMalformed},
		{"flipped R", vPayer, good, "CCB8" + vSig[4:], ErrBindingInvalid},
		{"flipped S", vPayer, good, vSig[:70] + "00" + vSig[72:], ErrBindingInvalid},
	}
	for _, c := range cases {
		if err := VerifyBinding(c.payer, c.msg, c.sig); err != c.wantErr {
			t.Errorf("%s: got %v want %v", c.name, err, c.wantErr)
		}
	}
}

func TestBindingRefusesNonCanonicalS(t *testing.T) {
	// S + L verifies in the cofactorless equation but is malleable: refuse.
	sig := make([]byte, 64)
	for i := 0; i < 64; i++ {
		v := 0
		for _, c := range []byte(vSig[i*2 : i*2+2]) {
			v <<= 4
			switch {
			case c >= 'A':
				v |= int(c-'A') + 10
			default:
				v |= int(c - '0')
			}
		}
		sig[i] = byte(v)
	}
	// L little-endian
	L := []byte{0xed, 0xd3, 0xf5, 0x5c, 0x1a, 0x63, 0x12, 0x58, 0xd6, 0x9c, 0xf7, 0xa2, 0xde, 0xf9, 0xde, 0x14, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10}
	carry := 0
	for i := 0; i < 32; i++ {
		s := int(sig[32+i]) + int(L[i]) + carry
		sig[32+i] = byte(s)
		carry = s >> 8
	}
	if carry != 0 {
		t.Skip("S+L overflows 256 bits for this vector")
	}
	msg, _ := BindingMessage(vHash, vNonce, vRes, vPay)
	hexs := ""
	for _, b := range sig {
		hexs += string("0123456789ABCDEF"[b>>4]) + string("0123456789ABCDEF"[b&15])
	}
	if err := VerifyBinding(vPayer, msg, hexs); err == nil {
		t.Fatal("malleable signature accepted")
	}
}

func TestDecodeAddress(t *testing.T) {
	if _, err := DecodeAddress("nano_3i1aq1cchnmbn9x5rsbap8b15akfh7wj7pwskuzi7ahz8oq6cobd99d4r3b7"); err != nil {
		t.Fatal(err)
	}
	good := vPayer
	for i := 5; i < len(good); i++ {
		for _, r := range nanoAlphabet {
			if byte(r) == good[i] {
				continue
			}
			if _, err := DecodeAddress(good[:i] + string(r) + good[i+1:]); err == nil {
				t.Fatalf("substitution at %d accepted", i)
			}
		}
	}
	for _, bad := range []string{"", "nano_", "btc_" + good[5:], good[:30], "nano_" + strings.Repeat("z", 60), good[:10] + "0" + good[11:]} {
		if _, err := DecodeAddress(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
