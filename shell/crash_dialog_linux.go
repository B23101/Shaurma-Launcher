//go:build linux

package main

// crash_dialog_linux.go — showStartupCrashDialog для Linux: нативний
// GTK4-діалог через прямий cgo-виклик, НЕ через Wails runtime/bridge.
// Той самий принцип, що crash_dialog_windows.go: паніка в main() може
// статись ДО того, як Wails runtime і bridge встигли піднятись, тому
// showFatalError (яка створює Wails-вікно) тут ще не спрацює — потрібен
// шлях, що не залежить від жодної частини застосунку, яка могла впасти.
//
// gtk_init() тут ідемпотентний і безпечний викликати повторно навіть
// якщо основний Wails/GTK-цикл уже частково ініціалізований — так
// документовано в GTK4 (повторні виклики gtk_init після першого — no-op).
// gtk_dialog_run — навмисно синхронний (блокує до закриття діалогу
// користувачем): це остання дія перед os.Exit(1) у main(), тому
// блокування головного потоку тут нешкідливе й навіть потрібне (інакше
// процес завершився б раніше, ніж користувач побачив би повідомлення).

/*
#cgo pkg-config: gtk4
#include <gtk/gtk.h>
#include <stdlib.h>

static void show_crash_dialog(const char* message) {
    if (!gtk_is_initialized()) {
        gtk_init();
    }
    GtkWidget *dialog = gtk_message_dialog_new(
        NULL,
        GTK_DIALOG_MODAL,
        GTK_MESSAGE_ERROR,
        GTK_BUTTONS_OK,
        "%s",
        message
    );
    gtk_window_set_title(GTK_WINDOW(dialog), "Shaurm Launcher — error");
    // GTK4 gtk_dialog_run був видалений в бік async — але для
    // one-shot стартового crash-діалогу (процес одразу завершується
    // після закриття) простіше й надійніше просто показати вікно і
    // прокрутити main loop, поки воно не закриється, ніж підписуватись
    // на "response" сигнал і городити callback-інфраструктуру заради
    // одного повідомлення, яке бачить хіба що розробник.
    g_signal_connect_swapped(dialog, "response", G_CALLBACK(gtk_window_destroy), dialog);
    gtk_window_present(GTK_WINDOW(dialog));
    while (GTK_IS_WIDGET(dialog) && gtk_widget_get_visible(dialog)) {
        g_main_context_iteration(NULL, TRUE);
    }
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func showStartupCrashDialog(stack string) {
	msg := fmt.Sprintf("Shaurm Launcher crashed during startup:\n\n%s\n\nCheck logs: ~/.shaurm/logs", stack)
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	C.show_crash_dialog(cmsg)
}
