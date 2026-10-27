// sanitizeHtml — білий список для СТОРОННЬОГО HTML/Markdown, який лаунчер
// вставляє через {@html}: описи CurseForge/Modrinth, вікі збірок з CDN.
//
// DOMParser розбирає HTML у пісочниці (скрипти не виконуються, ресурси не
// тягнуться), після чого з дерева видаляється все, чого немає у білому списку.
// Regex-підхід тут навмисно НЕ використовується: він лишав дірки з непарними
// <iframe>, <svg onload>, data:URL та encode-обходом (java&#x73;cript:).
//
// Дозволені теги — підмножина того, що генерує marked + типовий HTML
// CurseForge (p/div/h/ul/ol/li/table/img/a/b/i/s/code/pre/blockquote).
// Усе інше (script/style/svg/form/…) вирізається разом із вмістом.
// Атрибути: лише href/src/target/alt/title/class на дозволених тегах і лише
// http(s)/mailto/tel/якір/data:image URL. on*-, style-, srcdoc-, formaction-,
// xlink:href-атрибути — геть.
export interface SanitizeOptions {
	// Перетворювати YouTube-посилання/embed-и на постер із ▶ (клік монтує
	// плеєр). Потрібно лише там, де є обробник кліку (сторінка модпаку);
	// інакше лишаємо звичайний embed/посилання.
	videoPosters?: boolean
	// aria-label для постера відео (локалізований підпис).
	videoLabel?: string
}

// normalizeYouTubeEmbed — будь-яке посилання YouTube (watch?v=, embed/,
// shorts/, live/, youtu.be/, m.youtube.com, ?v= як query-параметр) →
// privacy-enhanced embed youtube-nocookie.com/embed/<id> БЕЗ query-параметрів,
// або '' якщо це не YouTube.
export function normalizeYouTubeEmbed(u: string): string {
	const m = /(?:youtube\.com\/(?:watch\?(?:[^#]*&)?v=|embed\/|shorts\/|live\/)|youtu\.be\/)([A-Za-z0-9_-]{6,})/i.exec(u)
	if (m) return `https://www.youtube-nocookie.com/embed/${m[1]}`
	const host = /^https?:\/\/([^/?#]+)/i.exec(u)?.[1]?.toLowerCase() ?? ''
	if (host === 'youtube.com' || host === 'www.youtube.com' || host.endsWith('.youtube.com')) {
		const qv = /[?&]v=([A-Za-z0-9_-]{6,})/i.exec(u)
		if (qv) return `https://www.youtube-nocookie.com/embed/${qv[1]}`
	}
	return ''
}

export function sanitizeHtml(html: string, opts: SanitizeOptions = {}): string {
	const videoPosters = opts.videoPosters ?? false
	const videoLabel = opts.videoLabel ?? 'Video'
	const allowedTags = new Set([
		'p', 'div', 'span', 'br', 'hr',
		'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
		'ul', 'ol', 'li', 'blockquote', 'pre', 'code',
		// input — лише чекбокси task lists (GFM-розмітка [-] / [x]);
		// нижче виріжеться все, крім type=checkbox+disabled.
		'input',
		'table', 'thead', 'tbody', 'tr', 'th', 'td',
		'img', 'a', 'b', 'strong', 'i', 'em', 'u', 's', 'del', 'sub', 'sup', 'small',
		// Вбудовані відео (YouTube/Vimeo) — ЛИШЕ з білого списку платформ
		// і лише атрибут src (див. iframeSrcAllowed).
		'iframe',
	])
	const safeUrl = (v: string) => /^(https?:|mailto:|tel:|#|\/|data:image\/(png|jpe?g|gif|webp);)/i.test(v.trim())
	const iframeSrcAllowed = (v: string) => {
		const m = /^https?:\/\/([^/]+)/i.exec(v.trim())
		if (!m) return false
		const host = m[1].toLowerCase()
		return host === 'www.youtube.com' || host === 'youtube.com' ||
			host === 'www.youtube-nocookie.com' || host === 'youtube-nocookie.com' ||
			host === 'youtu.be' || host === 'player.vimeo.com' || host === 'vimeo.com'
	}
	try {
		const doc = new DOMParser().parseFromString(html, 'text/html')
		const root = doc.body
		const walk = (el: Element) => {
			for (const child of Array.from(el.children)) {
				const tag = child.tagName.toLowerCase()
				if (!allowedTags.has(tag)) {
					child.remove()
					continue
				}
				// iframe: лишаємо ТІЛЬКИ src (і то з білого списку).
				if (tag === 'iframe') {
					const src = child.getAttribute('src') ?? ''
					if (!iframeSrcAllowed(src)) {
						child.remove()
						continue
					}
					for (const attr of Array.from(child.attributes)) {
						if (attr.name.toLowerCase() !== 'src') child.removeAttribute(attr.name)
					}
					const norm = normalizeYouTubeEmbed(src)
					if (norm) child.setAttribute('src', norm)
					child.setAttribute('loading', 'lazy')
					child.setAttribute('allowfullscreen', '')
					child.setAttribute('title', 'Video')
					child.setAttribute('referrerpolicy', 'no-referrer')
					walk(child)
					continue
				}
				// input: дозволений ЛИШЕ як чекбокс task list (GFM).
				if (tag === 'input') {
					const type = (child.getAttribute('type') ?? '').toLowerCase()
					if (type !== 'checkbox') {
						child.remove()
						continue
					}
					for (const attr of Array.from(child.attributes)) {
						const name = attr.name.toLowerCase()
						if (name !== 'type' && name !== 'checked' && name !== 'disabled') {
							child.removeAttribute(attr.name)
						}
					}
					child.setAttribute('disabled', '')
					continue
				}
				// YouTube-посилання → постер (лише якщо є обробник кліку).
				if (tag === 'a') {
					const href = child.getAttribute('href') ?? ''
					const embed = videoPosters ? normalizeYouTubeEmbed(href) : ''
					if (embed) {
						const id = /embed\/([A-Za-z0-9_-]{6,})/.exec(embed)?.[1] ?? ''
						const textLen = (child.textContent ?? '').trim().length
						if (id && textLen <= 60) {
							const poster = doc.createElement('div')
							poster.className = 'mp-body-video'
							poster.setAttribute('data-yt', id)
							poster.setAttribute('role', 'button')
							poster.setAttribute('tabindex', '0')
							const img = doc.createElement('img')
							img.src = `https://i.ytimg.com/vi/${id}/hqdefault.jpg`
							img.setAttribute('alt', '')
							img.setAttribute('loading', 'lazy')
							const play = doc.createElement('span')
							play.className = 'mp-body-video-play'
							const ic = doc.createElement('i')
							ic.className = 'ti ti-player-play-filled'
							play.appendChild(ic)
							poster.appendChild(img)
							poster.appendChild(play)
							child.replaceWith(poster)
							continue
						}
					}
				}
				// Чорний список атрибутів (виконувані/фішингові вектори).
				for (const attr of Array.from(child.attributes)) {
					const name = attr.name.toLowerCase()
					if (name.startsWith('on') || name === 'style' || name === 'srcdoc' ||
						name === 'formaction' || name === 'xlink:href' || name === 'xmlns' ||
						name === 'autoplay' || name === 'data-src') {
						child.removeAttribute(attr.name)
						continue
					}
					if (name === 'href' || name === 'src' || name === 'action') {
						const v = attr.value.trim().toLowerCase()
						if (!safeUrl(attr.value)) {
							child.removeAttribute(attr.name)
							continue
						}
						// encode-обхід: java&#x73;cript: / data:text/html.
						let dec = ''
						try { dec = decodeURIComponent(attr.value) } catch { dec = attr.value }
						if (/^\s*(javascript|vbscript|data:text\/html|data:application\/x)/i.test(dec) ||
							/^(java\s*script|vb\s*script|data\s*:\s*text\s*\/\s*html)/i.test(v)) {
							child.removeAttribute(attr.name)
							continue
						}
					}
					if (name === 'target' && attr.value.toLowerCase() !== '_blank') {
						child.removeAttribute(attr.name)
					}
				}
				// Посилання завжди у новій вкладці + без реферера.
				if (tag === 'a') {
					child.setAttribute('target', '_blank')
					child.setAttribute('rel', 'noreferrer noopener')
				}
				walk(child)
			}
		}
		walk(root)
		let out = root.innerHTML
		if (videoPosters) {
			// YouTube-iframe → постер (WebView2 масово віддає «Помилку 153» на
			// embed, завантажений одразу; плеєр монтується лише за кліком).
			out = out.replace(
				/<iframe[^>]*src="https:\/\/www\.youtube-nocookie\.com\/embed\/([A-Za-z0-9_-]{6,})"[^>]*><\/iframe>/gi,
				(_m, id: string) =>
					`<div class="mp-body-video" data-yt="${id}" role="button" tabindex="0" aria-label="${videoLabel}">` +
					`<img src="https://i.ytimg.com/vi/${id}/hqdefault.jpg" alt="" loading="lazy" />` +
					`<span class="mp-body-video-play"><i class="ti ti-player-play-filled"></i></span></div>`,
			)
		}
		return out
	} catch {
		// Будь-яка аномалія парсингу — не віддаємо сирий HTML.
		return ''
	}
}
