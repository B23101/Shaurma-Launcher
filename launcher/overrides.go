package main

// Типізовані merge-хелпери для злиття per-instance override-параметрів
// збірки з глобальними Settings (аудит: LaunchInstance дублював патерн
// «якщо override задано — override, інакше глобальне» дев'ять разів
// поспіль; нове override-поле тепер = один виклик хелпера, а не ще один
// if-блок). Семантика «не задано» залежить від типу: для рядка — порожній,
// для числа — 0, для буля — nil-вказівник.

// strOverride повертає override, якщо він непорожній, інакше base.
func strOverride(base, override string) string {
	if override != "" {
		return override
	}
	return base
}

// intOverride повертає override, якщо він > 0 (нуль = «не задано»),
// інакше base.
func intOverride(base, override int) int {
	if override > 0 {
		return override
	}
	return base
}

// boolPtrOverride повертає значення override-вказівника, якщо він не nil,
// інакше base.
func boolPtrOverride(base bool, override *bool) bool {
	if override != nil {
		return *override
	}
	return base
}
