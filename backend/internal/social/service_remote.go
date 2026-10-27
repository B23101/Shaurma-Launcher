package social

// service_remote.go — доступ віддаленого керування до Service: ключі E2E, дружба, відправка кадрів.
// Тут лише тонкі обгортки; уся логіка безпеки — у пакетах remotehost / remotectl / remotechan.

// RemoteHooks — обробники подій віддаленого керування (задаються в Deps).
type RemoteHooks struct {
	OnSnapshot   func([]RemoteOwnerList)
	OnList       func(RemoteOwnerList)
	OnCmd        func(RemoteIncoming)
	OnResult     func(RemoteResultFrame)
	OnStream     func(RemoteStreamFrame)
	OnStreamEnd  func(sid, reason string)
	OnVPSKey     func(pubKeyB64 string)
	OnP2PMandate func(P2PMandateFrame)
	OnPipe       func(PipeFrame)
	OnPipeEnd    func(tunnel, reason string)
}

func (d Deps) remoteHooks() RemoteHooks {
	if d.Remote == nil {
		return RemoteHooks{}
	}
	return *d.Remote
}

func (s *Service) presence() *PresenceClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pc
}

// RemoteCrypto — E2E-шифрування з друзями (nil, якщо чат вимкнено).
func (s *Service) RemoteCrypto() *ChatCrypto { return s.crypto }

// RemoteSelf — мій accountId (з підписаного JWT).
func (s *Service) RemoteSelf() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Me
}

// RemoteFriend — чи id є в моєму списку друзів і відколи (unix ms).
func (s *Service) RemoteFriend(id string) (bool, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.st.Friends {
		if f.AccountID == id {
			return true, f.Since
		}
	}
	return false, 0
}

// RemoteNick — нік друга (порожньо, якщо невідомо).
func (s *Service) RemoteNick(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.st.Friends {
		if f.AccountID == id {
			return f.Nickname
		}
	}
	return ""
}

func (s *Service) SendRemoteCmd(reqID, ownerID, serverID, op, sid, ct, nonce string) error {
	pc := s.presence()
	if pc == nil {
		return ErrNotConnected
	}
	return pc.SendRemoteCmd(reqID, ownerID, serverID, op, sid, ct, nonce)
}

func (s *Service) SendRemoteReply(reqID, toID, ct, nonce string) error {
	pc := s.presence()
	if pc == nil {
		return ErrNotConnected
	}
	return pc.SendRemoteReply(reqID, toID, ct, nonce)
}

func (s *Service) SendRemoteStream(sid, toID, ct, nonce string) error {
	pc := s.presence()
	if pc == nil {
		return ErrNotConnected
	}
	return pc.SendRemoteStream(sid, toID, ct, nonce)
}

func (s *Service) SendRemoteP2POpen(reqID, ownerID, serverID, eph string) error {
	pc := s.presence()
	if pc == nil {
		return ErrNotConnected
	}
	return pc.SendRemoteP2POpen(reqID, ownerID, serverID, eph)
}

// SetRemoteShares публікує на VPS, що я дозволяю друзям на моїх серверах.
func (s *Service) SetRemoteShares(list []RemoteShare) {
	if pc := s.presence(); pc != nil {
		pc.SetShares(list)
	}
}

// SetRemoteShareState оновлює стан уже опублікованого сервера.
func (s *Service) SetRemoteShareState(serverID, state string) {
	if pc := s.presence(); pc != nil {
		pc.SetShareState(serverID, state)
	}
}

// AssumeRemoteStale — див. PresenceClient.AssumeRemoteStale.
func (s *Service) AssumeRemoteStale() {
	if pc := s.presence(); pc != nil {
		pc.AssumeRemoteStale()
	}
}

func (s *Service) SendRemotePipe(tunnel, toID string, data []byte) error {
	pc := s.presence()
	if pc == nil {
		return ErrNotConnected
	}
	return pc.SendRemotePipe(tunnel, toID, data)
}
