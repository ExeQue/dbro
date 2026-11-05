//go:build linux

package main

/*
#cgo linux pkg-config: gtk+-3.0
#include <gtk/gtk.h>

// Wails' Wayland code leaves an xdg max size equal to the startup size, so
// Hyprland stretches the window instead of growing it (maximize, bigger tiles).
// Replace the hints with a min size only, which means "no maximum".
static gboolean dbro_unlock_size(gpointer data) {
	GList *list = gtk_window_list_toplevels();
	for (GList *l = list; l != NULL; l = l->next) {
		GdkGeometry g = {0};
		gtk_window_set_geometry_hints(GTK_WINDOW(l->data), NULL, &g, GDK_HINT_MIN_SIZE);
	}
	g_list_free(list);
	return G_SOURCE_REMOVE;
}

static void dbro_schedule_unlock(void) {
	g_idle_add(dbro_unlock_size, NULL);
}
*/
import "C"

// unlockWindowSize removes the maximum window size once the window is shown.
func unlockWindowSize() { C.dbro_schedule_unlock() }
