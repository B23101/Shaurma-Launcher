package share

import "testing"

func TestHostForgetFreesSlot(t *testing.T) {
	root := makeTree(t, 1000)
	h, err := NewHost(root, buildM(t, root), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var keys []string
	for _, id := range []string{"a", "b", "c"} {
		k, err := h.AddRecipient(id)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	if _, err := h.AddRecipient("d"); err != ErrTooManyRecipients {
		t.Fatalf("четвертий одночасно має відхилятись: %v", err)
	}
	if h.Forget("a") {
		t.Fatal("не можна забути того, хто ще не завершив")
	}
	h.Revoke("a") // фінальний стан
	if !h.Forget("a") {
		t.Fatal("завершеного можна забути")
	}
	if h.get("a") != nil || h.byKey[keys[0]] != nil {
		t.Fatal("ключ і запис мають зникнути")
	}
	if _, err := h.AddRecipient("d"); err != nil {
		t.Fatalf("слот звільнено: %v", err)
	}
	if h.Forget("nobody") {
		t.Fatal("невідомого забути не можна")
	}
}
