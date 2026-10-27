package social

import "testing"

func TestSendStateOnlyFailsOnExplicitRefusal(t *testing.T) {
	var events []string
	s := &Service{d: Deps{Emit: func(name string, _ any) { events = append(events, name) }}}

	if s.ChatSendState("c1").Status != "unknown" {
		t.Fatal("невідомий ID → unknown")
	}
	// повільне/відкладене підтвердження: поки його немає — це pending, а не «не надіслано»
	s.setSendState("c1", SendState{Status: "pending", ChatID: "a_b"})
	if st := s.ChatSendState("c1"); st.Status != "pending" {
		t.Fatalf("pending: %+v", st)
	}
	// підтвердження прийшло пізно — стан стає acked (UI більше не показує помилку)
	s.onMessageAck(MessageAck{ClientMessageID: "c1", ChatID: "a_b", ID: 7, TS: 99})
	if st := s.ChatSendState("c1"); st.Status != "acked" || st.ID != 7 {
		t.Fatalf("acked: %+v", st)
	}
	// явна відмова сервера з кодом
	s.setSendState("c2", SendState{Status: "pending"})
	s.onMessageFailed("c2", "not_friends")
	if st := s.ChatSendState("c2"); st.Status != "failed" || st.Code != "not_friends" {
		t.Fatalf("failed: %+v", st)
	}
	if len(events) != 2 || events[0] != "chat:ack" || events[1] != "chat:failed" {
		t.Fatalf("події: %v", events)
	}
}
