package main

// activity_share_shaurma.go — передачі збірок/серверів друзям у реєстрі
// довгих операцій (activity_api.go). Окремо, бо share є лише в збірці Shaurma.

import (
	"fmt"

	"shaurma-launcher-backend/internal/activity"
	"shaurma-launcher-backend/internal/share"
)

func (a *App) observeShare(name string, payload any) {
	switch v := payload.(type) {
	case ShareInView:
		a.observeShareIn(v)
	case ShareOutView:
		a.observeShareOut(v)
	}
}

func (a *App) observeShareIn(v ShareInView) {
	id := actShareIn + ":" + v.ID
	it := activity.Activity{ID: id, Kind: actShareIn, Title: v.Name, Target: v.ID, Detail: v.From, Percent: -1}
	switch v.Phase {
	case "downloading":
		total := v.Progress.BytesTotal
		if total <= 0 {
			total = v.Bytes
		}
		it.Percent = pctOf64(v.Progress.BytesDone, total)
		if v.Progress.Reconnecting {
			it.State = activity.StatePaused
		}
	case "paused":
		it.State = activity.StatePaused
		it.Percent = pctOf64(v.Progress.BytesDone, v.Progress.BytesTotal)
	case "done":
		a.actFinish(id, "")
		return
	case "error":
		a.actFinish(id, firstNonEmpty(v.Error, v.Reason, "error"))
		return
	case "declined", "cancelled":
		a.actRemove(id)
		return
	}
	a.actUpdate(it)
}

func (a *App) observeShareOut(v ShareOutView) {
	id := actShareOut + ":" + v.ID
	it := activity.Activity{ID: id, Kind: actShareOut, Title: v.Name, Target: v.ID, Percent: -1}
	switch v.Phase {
	case "preparing":
		it.Percent = pctOf(v.Hashed, v.HashTotal)
	case "ready":
		// Як у віджеті передач: середній % по одержувачах, що не відмовились.
		sum, n, active, final := 0, 0, 0, 0
		for _, r := range v.Recipients {
			if share.IsFinalState(r.State) {
				final++
			}
			if r.State == share.StateTransferring {
				active++
			}
			if r.State == share.StateDeclined || r.State == share.StateRevoked {
				continue
			}
			n++
			if r.State == share.StateDone {
				sum += 100
			} else {
				sum += pctOf64(r.BytesDone, v.TotalBytes)
			}
		}
		if len(v.Recipients) > 0 && final == len(v.Recipients) {
			a.actFinish(id, "")
			return
		}
		if n > 0 {
			it.Percent = sum / n
		}
		if active == 0 {
			// Передача відкрита, але друзі ще не качають — не займаємо таскбар.
			it.State = activity.StateWaiting
		}
		it.Detail = fmt.Sprintf("%d/%d", final, len(v.Recipients))
	case "closed":
		a.actFinish(id, "")
		return
	case "error":
		a.actFinish(id, firstNonEmpty(v.Error, "error"))
		return
	}
	a.actUpdate(it)
}
