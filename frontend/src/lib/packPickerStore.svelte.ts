// Спільні дані списку збірок з папками (той самий порядок, що в бічній
// панелі). App.svelte оновлює їх, а будь-яке вікно вибору збірки
// (PackChooser у міграції модів тощо) читає звідси — без прокидання props
// через пів застосунку і без власного «спрощеного» списку.
import type { ModTargetPicker } from './modTarget'

export const sharedPackPicker = $state<{ value: ModTargetPicker | null }>({ value: null })
