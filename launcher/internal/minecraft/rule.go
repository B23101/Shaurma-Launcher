package minecraft

import "encoding/json"

// Rule представляє правило активації бібліотеки з Mojang version.json.
// Портовано з Prism Launcher (launcher/minecraft/Rule.h/cpp)
// для забезпечення 100% сумісності логіки визначення активності.
//
// Приклад JSON:
//   {
//     "action": "allow",
//     "os": {
//       "name": "linux"
//     }
//   }
//
// Логіка (ТОЧНО як у Prism):
//   - Якщо rules порожній масив → бібліотека активна
//   - Кожне правило повертає Allow, Disallow або Defer
//   - Defer означає "не застосовувати це правило" (OS не співпала)
//   - ОСТАННЄ не-Defer правило визначає результат
//   - За замовчуванням (якщо всі Defer) → Disallow
type Rule struct {
	// Action - дія правила: "allow" або "disallow"
	Action string `json:"action"`
	
	// OS - умова по операційній системі (опціонально)
	OS *RuleOS `json:"os,omitempty"`
	
	// Features - умова по features (опціонально, не використовується зараз)
	Features map[string]bool `json:"features,omitempty"`
}

// RuleOS представляє умову по операційній системі.
type RuleOS struct {
	// Name - назва OS: "windows", "linux", "osx"
	Name string `json:"name,omitempty"`
	
	// Version - regex для версії OS (не підтримується, залишено для сумісності)
	Version string `json:"version,omitempty"`
	
	// Arch - архітектура: "x86", "x86_64" (рідко використовується)
	Arch string `json:"arch,omitempty"`
}

// RuleAction представляє результат застосування правила.
// Портовано з Prism: Rule::Action enum
type RuleAction int

const (
	// RuleAllow - дозволити бібліотеку
	RuleAllow RuleAction = iota
	
	// RuleDisallow - заборонити бібліотеку
	RuleDisallow
	
	// RuleDefer - пропустити це правило (умова не виконана)
	RuleDefer
)

// Apply застосовує правило до RuntimeContext і повертає дію.
// Портовано з Prism: Rule::apply(const RuntimeContext&)
//
// Логіка (ТОЧНО як у Prism):
//   1. Якщо вказана OS і вона НЕ співпадає → Defer (пропустити правило)
//   2. Інакше → повернути Allow або Disallow (залежно від action)
//
// Приклади:
//   Rule{Action: "allow", OS: {Name: "linux"}} + RuntimeContext{System: "linux"} → Allow
//   Rule{Action: "allow", OS: {Name: "linux"}} + RuntimeContext{System: "windows"} → Defer
//   Rule{Action: "disallow", OS: {Name: "osx"}} + RuntimeContext{System: "osx"} → Disallow
func (r *Rule) Apply(ctx *RuntimeContext) RuleAction {
	// Перевірка умови OS
	if r.OS != nil && r.OS.Name != "" {
		// Якщо OS не співпадає → Defer (пропустити це правило)
		if !ctx.ClassifierMatches(r.OS.Name) {
			return RuleDefer
		}
	}
	
	// Умова виконана (або умови немає) → повернути action
	switch r.Action {
	case "allow":
		return RuleAllow
	case "disallow":
		return RuleDisallow
	default:
		return RuleDefer
	}
}

// ApplyRules застосовує масив правил і повертає фінальний результат.
// Портовано з Prism логіки у Library::isActive()
//
// Логіка (ТОЧНО як у Prism):
//   1. За замовчуванням result = Disallow
//   2. Для кожного правила викликаємо Apply()
//   3. Якщо результат НЕ Defer → зберігаємо його (перезаписуємо)
//   4. ОСТАННЄ не-Defer правило виграє
//   5. Якщо всі правила Defer → залишається Disallow
//
// Приклади:
//   []Rule{} → Allow (порожній масив = без обмежень)
//   
//   []Rule{
//     {Action: "allow", OS: {Name: "linux"}},
//   } + linux → Allow
//   
//   []Rule{
//     {Action: "allow", OS: {Name: "linux"}},
//   } + windows → Disallow (правило не спрацювало, за замовчуванням Disallow)
//   
//   []Rule{
//     {Action: "allow"},
//     {Action: "disallow", OS: {Name: "osx"}},
//   } + osx → Disallow (останнє правило перевизначає)
func ApplyRules(rules []Rule, ctx *RuntimeContext) bool {
	// Порожній масив = без обмежень = активна
	if len(rules) == 0 {
		return true
	}
	
	// За замовчуванням Disallow (ТОЧНО як у Prism)
	result := RuleDisallow
	
	// Застосовуємо кожне правило
	for _, rule := range rules {
		action := rule.Apply(ctx)
		
		// Якщо не Defer → зберігаємо результат
		// ОСТАННЄ не-Defer правило виграє (як у Prism)
		if action != RuleDefer {
			result = action
		}
	}
	
	// Повертаємо true якщо Allow
	return result == RuleAllow
}

// ParseRules парсить масив правил з JSON.
func ParseRules(data []byte) ([]Rule, error) {
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}
