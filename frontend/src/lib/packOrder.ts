// packOrder.ts — ЄДИНЕ джерело правди для ПОРЯДКУ збірок у списках.
//
// Раніше кожне місце зі списком збірок сортувало по-своєму: sidebar
// використовував orderedBuildViews (закріплені → ручний порядок → нові),
// а модалка «Копіювати сервер в іншу збірку» брала сирий масив з бекенду —
// звідси розбіжності. Тепер порядок скрізь один:
//
//   1. Закріплені (packLayout.pinned) — у порядку закріплення.
//   2. Незакріплені (packLayout.order) — у ручному порядку.
//   3. Збірки з папок — на місці своєї папки (порядок папок = packFolders),
//      всередині папки — у порядку packIds (як у sidebar-акордеоні).
//   4. Нові (без запису в layout) — в кінці, як прийшли з бекенду.
//
// Дві функції відрізняються лише тим, чи входять у результат збірки з
// папок: main-список (sidebar/сітка) їх ховає (вони живуть у папках), а
// flat-список (копіювання сервера, глобальний пошук) показує їх на місці
// папки.
import type { BuildView, PackFolder } from './types'

// deriveServerFolders — будує «серверні» PackFolder з folderId/folderName/
// folderColor/folderOrder/folderPosition, які Pack Manager проставив
// збіркам у packs-index.json (ТЗ: «групування по кастомній папці... тоді
// лаунчер буде показувати папку з тими збірками з її назвою і кольором»).
// Викликається на КОЖЕН GetBuilds() — членство/назва/колір завжди
// синхронні з сервером, локально нічого не зберігається (на відміну від
// packFolders, куди пишуться лише user-created папки).
//
// name/color беруться з ПЕРШОЇ (за folderOrder) збірки групи — Pack
// Manager пише їх однаково в усіх збірках однієї папки, тому будь-який
// запис підійшов би, але детермінований вибір (мінімальний folderOrder,
// далі мінімальний id) важливий: без нього назва/колір папки могли б
// «блимати» між перечитуваннями, якщо порядок builds від бекенду не
// стабільний.
export function deriveServerFolders(builds: BuildView[]): PackFolder[] {
  const groups = new Map<string, BuildView[]>()
  for (const bv of builds) {
    const fid = bv.build.folderId
    if (!fid) continue
    const arr = groups.get(fid)
    if (arr) arr.push(bv)
    else groups.set(fid, [bv])
  }

  const folders: { folder: PackFolder; position: number }[] = []
  for (const [fid, members] of groups) {
    members.sort((a, b) => {
      const ao = a.build.folderOrder ?? 0, bo = b.build.folderOrder ?? 0
      if (ao !== bo) return ao - bo
      return a.build.id.localeCompare(b.build.id)
    })
    const head = members[0]
    folders.push({
      folder: {
        id: fid,
        name: head.build.folderName || fid,
        color: head.build.folderColor,
        packIds: members.map((m) => m.build.id),
        serverManaged: true,
      },
      position: head.build.folderPosition ?? 0,
    })
  }
  folders.sort((a, b) => a.position - b.position)
  return folders.map((f) => f.folder)
}

// mergeFolders — об'єднує серверні папки (завжди свіжі з builds) з
// локальними user-created (packFolders у settings.json). Якщо якийсь id
// одночасно локальний і серверний (напр. Pack Manager щойно перетворив
// раніше кастомну папку на офіційну під тим самим id) — перемагає
// серверна версія: сервер є джерелом правди для членства/назви/кольору
// такої папки, локальний запис для цього id ігнорується (але НЕ
// видаляється з локального стореджу — якщо збірку прибрати з серверної
// папки пізніше, локальні дані не губляться самі по собі).
export function mergeFolders(local: PackFolder[], server: PackFolder[]): PackFolder[] {
  const serverIds = new Set(server.map((f) => f.id))
  const localOnly = local.filter((f) => !serverIds.has(f.id))
  return [...server, ...localOnly]
}

export interface PackLayoutLike {
  order: string[]
  pinned: string[]
}

// byId — карта id → BuildView.
function byIdMap(builds: BuildView[]): Map<string, BuildView> {
  return new Map(builds.map((bv) => [bv.build.id, bv]))
}

function folderPackSet(folders: PackFolder[]): Set<string> {
  return new Set(folders.flatMap((f) => f.packIds))
}

// orderMainBuildViews — порядок для ГОЛОВНОГО списку (sidebar/сітка):
// збірки з папок не показуються (вони всередині папок).
export function orderMainBuildViews(
  builds: BuildView[],
  layout: PackLayoutLike,
  folders: PackFolder[]
): BuildView[] {
  const byId = byIdMap(builds)
  const pinnedSet = new Set(layout.pinned)
  const orderKnown = new Set(layout.order)
  const inFolder = folderPackSet(folders)

  // Закріплені — у порядку layout.pinned; збірки з папок не можуть бути
  // закріплені, але на випадок битих даних відсіваємо їх і тут.
  const pinned = layout.pinned
    .map((id) => byId.get(id))
    .filter((b): b is BuildView => !!b && !inFolder.has(b.build.id))

  // Незакріплені зі збереженим порядком — layout.order, без pinned і папок.
  const ordered = layout.order
    .filter((id) => !pinnedSet.has(id) && !inFolder.has(id))
    .map((id) => byId.get(id))
    .filter((b): b is BuildView => !!b)

  // Нові (не в order і не в pinned, не в папках) — в кінці.
  const rest = builds.filter(
    (bv) => !orderKnown.has(bv.build.id) && !pinnedSet.has(bv.build.id) && !inFolder.has(bv.build.id)
  )

  return [...pinned, ...ordered, ...rest]
}

// orderFlatBuildViews — порядок для ПЛОСКИХ списків (модалка копіювання
// сервера, глобальний пошук тощо): те саме, що й у sidebar, але збірки з
// папок стоять НА МІСЦІ своєї папки (порядок папок = packFolders),
// усередині папки — у порядку packIds.
export function orderFlatBuildViews(
  builds: BuildView[],
  layout: PackLayoutLike,
  folders: PackFolder[]
): BuildView[] {
  const byId = byIdMap(builds)
  const pinnedSet = new Set(layout.pinned)
  const orderKnown = new Set(layout.order)
  const inFolder = folderPackSet(folders)

  const pinned = layout.pinned
    .map((id) => byId.get(id))
    .filter((b): b is BuildView => !!b && !inFolder.has(b.build.id))

  const ordered = layout.order
    .filter((id) => !pinnedSet.has(id) && !inFolder.has(id))
    .map((id) => byId.get(id))
    .filter((b): b is BuildView => !!b)

  // Збірки з папок — у порядку папок (packFolders), усередині папки — у
  // порядку packIds. Неіснуючі ID відсіваються.
  const folderPacks: BuildView[] = []
  for (const f of folders) {
    for (const pid of f.packIds) {
      const bv = byId.get(pid)
      if (bv) folderPacks.push(bv)
    }
  }

  const folderPackIds = new Set(folderPacks.map((b) => b.build.id))
  const rest = builds.filter(
    (bv) =>
      !orderKnown.has(bv.build.id) &&
      !pinnedSet.has(bv.build.id) &&
      !inFolder.has(bv.build.id) &&
      !folderPackIds.has(bv.build.id)
  )

  return [...pinned, ...ordered, ...folderPacks, ...rest]
}
