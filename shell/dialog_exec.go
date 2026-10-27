package main

// dialog_exec.go — системні діалоги (вибір файлів/теки, збереження файлу).
// Shell приймає options_json і будує нативний Wails-діалог; результат —
// шлях(и) або "" при скасуванні. ВАЖЛИВО: діалоги викликаються ЛИШЕ з
// не-UI потоків (диспетчер Bridge.Invoke або горутини DLL) — сам Wails
// переносить показ на головний потік, як і в оригінальному лаунчері.

import (
	"encoding/json"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// dialogOpts — формат options_json (bridge.h): {"title":..,"directories":..,
// "defaultName":..,"filters":[{"name":..,"extensions":[..]}]}.
type dialogOpts struct {
	Title       string `json:"title"`
	Directories bool   `json:"directories"`
	DefaultName string `json:"defaultName"`
	Filters     []struct {
		Name       string   `json:"name"`
		Extensions []string `json:"extensions"`
	} `json:"filters"`
}

func parseDialogOpts(optsJSON string) dialogOpts {
	var o dialogOpts
	_ = json.Unmarshal([]byte(optsJSON), &o)
	return o
}

// filterPattern перетворює ["png","jpg"] -> "*.png;*.jpg" (формат Wails).
func filterPattern(exts []string) string {
	if len(exts) == 0 {
		return ""
	}
	for i, e := range exts {
		exts[i] = "*." + strings.TrimPrefix(e, ".")
	}
	return strings.Join(exts, ";")
}

// dialogOpenFile — один файл або тека ("directories":true).
func dialogOpenFile(optsJSON string) string {
	o := parseDialogOpts(optsJSON)
	app := application.Get()
	if app == nil {
		return ""
	}
	dlg := app.Dialog.OpenFile()
	if o.Title != "" {
		dlg.SetTitle(o.Title)
	}
	dlg.CanChooseDirectories(o.Directories)
	for _, f := range o.Filters {
		if p := filterPattern(f.Extensions); p != "" {
			dlg.AddFilter(f.Name, p)
		}
	}
	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return path
}

// dialogOpenFiles — КІЛЬКА файлів (масив шляхів у JSON).
func dialogOpenFiles(optsJSON string) string {
	o := parseDialogOpts(optsJSON)
	app := application.Get()
	if app == nil {
		return "[]"
	}
	dlg := app.Dialog.OpenFile()
	if o.Title != "" {
		dlg.SetTitle(o.Title)
	}
	for _, f := range o.Filters {
		if p := filterPattern(f.Extensions); p != "" {
			dlg.AddFilter(f.Name, p)
		}
	}
	files, err := dlg.PromptForMultipleSelection()
	if err != nil || files == nil {
		files = []string{}
	}
	b, _ := json.Marshal(files)
	return string(b)
}

// dialogSaveFile — збереження файлу (defaultName — підставлене ім'я).
func dialogSaveFile(optsJSON string) string {
	o := parseDialogOpts(optsJSON)
	app := application.Get()
	if app == nil {
		return ""
	}
	dlg := app.Dialog.SaveFile()
	// SaveFileDialogStruct не має SetTitle (тільки OpenFileDialog) —
	// назва діалогу задається через defaultName + кнопки системно.
	if o.DefaultName != "" {
		dlg.SetFilename(o.DefaultName)
	}
	for _, f := range o.Filters {
		if p := filterPattern(f.Extensions); p != "" {
			dlg.AddFilter(f.Name, p)
		}
	}
	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return path
}
