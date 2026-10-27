// Іконки авто-збірок «Vanilla / Snapshot» (файли поклав користувач:
// vanilla_icon.png і shaphot_icon.png). Vite-суфікс «?inline» вбудовує
// картинку в JS-бандл як data-URL — бекенд не має доступу до файлів
// фронтенд-асетів на диску, тож EnsureAutoVersionPacks приймає PNG саме
// таким data-URL і пише його в теку збірки (icon.png).
import vanillaDataURL from '../assets/images/vanilla_icon.png?inline'
import shaphotDataURL from '../assets/images/shaphot_icon.png?inline'

export const autoPackIcons = {
  vanilla: vanillaDataURL,
  shaphot: shaphotDataURL,
} as const
