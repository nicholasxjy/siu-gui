// Command siu-gui is the mygo desktop shell: it binds the Desktop service
// (see desktop.go and docs/protocol.md) and shows the frontend in a single
// native window with platform window controls and a custom 48px header.
package main

import (
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/egoist/mygo"

	"github.com/nicholasxjy/siu-gui/internal/backend"
)

// mainWindowState guards the single window siu-gui shows: openMainWindow
// reuses it if still open, so a second launch or a Dock click never opens
// a duplicate. Main-thread only in practice (every caller runs from a
// mygo callback), but guarded anyway since OnSecondInstance delivers on
// the main thread too but from a background listener goroutine's post.
var mainWindowState struct {
	mu sync.Mutex
	w  *mygo.Window
}

func main() {
	desktop := &Desktop{}
	mygo.BindAs("Desktop", desktop)

	if os.Getenv("MYGO_GENERATE") != "" {
		// `mygo generate` (and `mygo dev`/`mygo build` writing the client
		// before their own build) run main with MYGO_GENERATE set so that
		// App.Run only writes the TypeScript client and returns: no
		// window, no single-instance lock, no siu-core process. Bind
		// above is enough for that; nothing else below may run.
		if err := mygo.App.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}

	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		openMainWindow()
	})

	mygo.App.WhenReady(func() {
		resourcesDir, _ := mygo.App.Path(mygo.PathResources)
		desktop.setClient(backend.NewWithExecutable(resolveCoreExecutablePath(resourcesDir)))

		setupMenu()
		openMainWindow()
	})

	if runtime.GOOS == "darwin" {
		// Standard macOS behavior: closing the window leaves the app
		// running in the Dock, and clicking the Dock icon again reopens
		// it instead of quitting.
		mygo.App.OnWindowAllClosed(func() {})
		mygo.App.OnActivate(func(hasVisibleWindows bool) {
			if !hasVisibleWindows {
				openMainWindow()
			}
		})
	}

	mygo.App.OnQuit(func() {
		desktop.closeClient()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// setupMenu installs the standard application menu: Edit (and the roles
// mac bundles with it) work the same on every platform, so Cut/Copy/Paste/
// Undo/Redo/Select All and Cmd+Q (mac) keep their usual shortcuts even
// though the window draws its own header instead of a native title bar.
// Roles that only exist on macOS (RoleAppMenu, RoleWindowMenu's own
// RoleWindow submenu is not used here) are dropped automatically on other
// platforms, so this one template is correct everywhere.
func setupMenu() {
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	}))
}

// openMainWindow shows the app's single window, creating it on first use
// (or after the user closed it on macOS, where the app stays running) and
// otherwise just bringing the existing one forward -- used by the first
// launch, OnActivate (Dock click with no visible windows) and
// OnSecondInstance (another launch handed its command line to us).
func openMainWindow() {
	mainWindowState.mu.Lock()
	w := mainWindowState.w
	mainWindowState.mu.Unlock()

	if w != nil && !w.IsDestroyed() {
		w.Show()
		if w.IsMinimized() {
			w.Restore()
		}
		w.Focus()
		return
	}

	nw := mygo.NewWindow(mygo.WindowOptions{
		Title:  "Siu",
		URL:    "/",
		Width:  1440,
		Height: 900,
		// MinWidth/MinHeight keep the fixed sidebar and content layout
		// usable; the frontend does not support anything smaller.
		MinWidth:  960,
		MinHeight: 680,
		// TitleBarHidden keeps the OS window controls (traffic lights /
		// minimize-maximize-close) but gives the page the rest of the
		// window; the frontend draws its own 48px header under them and
		// keeps clear of their --mygo-titlebar-* reserved space.
		TitleBarStyle:  mygo.TitleBarHidden,
		TitleBarHeight: 48,
		// AutoHideMenuBar: the app menu (setupMenu) exists for its
		// shortcuts and macOS's menu bar, not to be shown permanently
		// over the custom header on Windows/Linux; Alt or F10 still
		// opens it there.
		AutoHideMenuBar: true,
		// StateKey remembers position, size and maximized/full screen
		// state across launches.
		StateKey: "main",
		// Matches --stage from src/styles.css so the window never shows
		// a flash of the wrong color while the page loads.
		BackgroundColor: "light-dark(#e7eaf1, #191c25)",
	})

	mainWindowState.mu.Lock()
	mainWindowState.w = nw
	mainWindowState.mu.Unlock()
}
