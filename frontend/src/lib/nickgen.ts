// Генератор осмислених ніків для Minecraft: «Прикметник + Іменник» (CosmicFox, BraveOtter7).
// Лише a-zA-Z0-9_, 3–16 символів. Криптовипадковість — щоб два гравці не діставали однаковий набір.

const ADJ = [
  'Brave', 'Swift', 'Cosmic', 'Mystic', 'Lucky', 'Silent', 'Golden', 'Frosty', 'Stormy', 'Sunny',
  'Cozy', 'Wild', 'Noble', 'Clever', 'Mighty', 'Gentle', 'Fiery', 'Shiny', 'Sleepy', 'Happy',
  'Crimson', 'Azure', 'Lunar', 'Solar', 'Rusty', 'Misty', 'Atomic', 'Turbo', 'Pixel', 'Neon',
  'Royal', 'Ancient', 'Wandering', 'Hidden', 'Rapid', 'Jolly', 'Daring', 'Epic', 'Funky', 'Salty',
]
const NOUN = [
  'Fox', 'Wolf', 'Otter', 'Panda', 'Falcon', 'Dragon', 'Raven', 'Tiger', 'Badger', 'Lynx',
  'Knight', 'Wizard', 'Miner', 'Ranger', 'Pirate', 'Golem', 'Creeper', 'Axolotl', 'Phoenix', 'Bear',
  'Comet', 'Anchor', 'Compass', 'Lantern', 'Pickaxe', 'Cobweb', 'Ember', 'Breeze', 'Glacier', 'Harbor',
  'Hawk', 'Owl', 'Shark', 'Turtle', 'Bee', 'Cactus', 'Pebble', 'Maple', 'Orbit', 'Nomad',
]

function rnd(n: number): number {
  const a = new Uint32Array(1)
  crypto.getRandomValues(a)
  return a[0] % n
}

/** Один варіант ніка. withNumber — додати 1–2 цифри (менше колізій). */
export function generateNick(withNumber = false): string {
  for (let i = 0; i < 20; i++) {
    let n = ADJ[rnd(ADJ.length)] + NOUN[rnd(NOUN.length)]
    if (withNumber) n += String(1 + rnd(99))
    if (n.length <= 16) return n
  }
  return NOUN[rnd(NOUN.length)] + String(10 + rnd(90))
}
