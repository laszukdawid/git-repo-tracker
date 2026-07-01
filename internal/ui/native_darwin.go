//go:build darwin

package ui

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa -framework QuartzCore
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <dispatch/dispatch.h>

// findWindow returns the app's NSWindow whose title matches needle, or nil.
static NSWindow *GRTFindWindow(NSString *needle) {
	for (NSWindow *win in [NSApp windows]) {
		if ([[win title] isEqualToString:needle]) {
			return win;
		}
	}
	return nil;
}

// GRTKillLayerAnimations disables Core Animation's implicit actions on a view's
// layer and every descendant. The popover's content view is layer-backed (for the
// rounded corners), and a layer-backed view animates its bounds/position by default
// — so a programmatic resize (group collapse/expand, live search filtering) would
// interpolate the old frame into the new bounds, i.e. briefly scale ("zoom") the
// whole popover. Pinning contents top-left and nulling the geometry actions makes
// every resize snap cleanly instead. Re-applied on each show in case Fyne rebuilt
// the view tree.
static void GRTKillLayerAnimations(NSView *view) {
	CALayer *layer = [view layer];
	if (layer != nil) {
		layer.contentsGravity = kCAGravityTopLeft;
		layer.actions = @{
			@"bounds":     [NSNull null],
			@"position":   [NSNull null],
			@"contents":   [NSNull null],
			@"sublayers":  [NSNull null],
			@"onOrderIn":  [NSNull null],
			@"onOrderOut": [NSNull null],
		};
	}
	for (NSView *sub in [view subviews]) {
		GRTKillLayerAnimations(sub);
	}
}

// GRTPlacePopover anchors the window just below the menu bar, horizontally
// centered under the tray icon. Neither Fyne nor systray exposes the status
// item's position, so we approximate it with the cursor location at click time
// (the pointer is over the icon when the user clicks it). The mouse location is
// read synchronously before the async block, and the title is copied into an
// NSString, so we never touch click-time/Go-owned state after this returns.
static void GRTPlacePopover(const char *title, double width, double height) {
	NSString *needle = [[NSString alloc] initWithUTF8String:title];
	NSPoint mouse = [NSEvent mouseLocation];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *win = GRTFindWindow(needle);
		if (win == nil) {
			return;
		}
		// Pick the screen whose menu bar was clicked (multi-monitor aware).
		NSScreen *screen = nil;
		for (NSScreen *s in [NSScreen screens]) {
			if (NSPointInRect(mouse, [s frame])) {
				screen = s;
				break;
			}
		}
		if (screen == nil) {
			screen = [NSScreen mainScreen];
		}
		if (screen != nil) {
			NSRect visible = [screen visibleFrame];
			double margin = 6.0;
			double x = mouse.x - width / 2.0; // center under the icon/cursor
			if (x + width > NSMaxX(visible) - margin) {
				x = NSMaxX(visible) - width - margin;
			}
			if (x < NSMinX(visible) + margin) {
				x = NSMinX(visible) + margin;
			}
			double y = NSMaxY(visible) - height - margin; // just under the menu bar
			[win setFrame:NSMakeRect(x, y, width, height) display:YES animate:NO];
			[win setLevel:NSStatusWindowLevel];
		}
		// Round the window corners. The content view's layer clips its (opaque)
		// GL subview to a rounded rect; the corners outside it fall back to the
		// window's clear background, so they're transparent — giving real rounded
		// corners plus a drop shadow, without needing a transparent GL framebuffer.
		[win setOpaque:NO];
		[win setBackgroundColor:[NSColor clearColor]];
		[win setHasShadow:YES];
		NSView *cv = [win contentView];
		if (cv != nil) {
			[cv setWantsLayer:YES];
			cv.layer.cornerRadius = 20.0;
			cv.layer.masksToBounds = YES;
			GRTKillLayerAnimations(cv); // no implicit scale animation on resize
		}
		[NSApp activateIgnoringOtherApps:YES];
		[win makeKeyAndOrderFront:nil];
	});
}

// GRTResizePopover changes the popover's height (and width) while keeping its
// top-left corner fixed, so it grows/shrinks downward from just below the menu
// bar. Unlike GRTPlacePopover it never reads the cursor, so it can run while the
// user types in the search field without the window hopping to the pointer.
static void GRTResizePopover(const char *title, double width, double height) {
	NSString *needle = [[NSString alloc] initWithUTF8String:title];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *win = GRTFindWindow(needle);
		if (win == nil) {
			return;
		}
		NSRect f = [win frame];
		double top = NSMaxY(f);  // current top edge (origin is bottom-left)
		double y = top - height; // keep the top fixed; extend/retract the bottom
		// Suppress implicit layer animations for this geometry change too, so the
		// resize snaps instead of scaling the old content into the new bounds.
		[CATransaction begin];
		[CATransaction setDisableActions:YES];
		[win setFrame:NSMakeRect(f.origin.x, y, width, height) display:YES animate:NO];
		[CATransaction commit];
	});
}

// GRTApplyVibrancy is best-effort macOS blur. It often won't show through Fyne's
// opaque GL canvas, so it is gated behind an env var rather than enabled by default.
static void GRTApplyVibrancy(const char *title) {
	NSString *needle = [[NSString alloc] initWithUTF8String:title];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *win = GRTFindWindow(needle);
		if (win == nil) {
			return;
		}
		[win setOpaque:NO];
		[win setBackgroundColor:[NSColor clearColor]];
		NSView *content = [win contentView];
		if (content == nil) {
			return;
		}
		for (NSView *sub in [content subviews]) {
			if ([sub isKindOfClass:[NSVisualEffectView class]]) {
				return; // already applied
			}
		}
		NSVisualEffectView *fx = [[NSVisualEffectView alloc] initWithFrame:[content bounds]];
		[fx setAutoresizingMask:NSViewWidthSizable | NSViewHeightSizable];
		[fx setMaterial:NSVisualEffectMaterialHUDWindow];
		[fx setBlendingMode:NSVisualEffectBlendingModeBehindWindow];
		[fx setState:NSVisualEffectStateActive];
		[content addSubview:fx positioned:NSWindowBelow relativeTo:nil];
	});
}

// grtAppResignedActive is the Go callback (defined via //export in a companion
// file) invoked when the app loses active status.
extern void grtAppResignedActive(void);

// GRTWatchDeactivate fires grtAppResignedActive whenever the app resigns active
// (the user clicked outside it), so the popover can auto-dismiss. Registered once.
static BOOL grtDeactivateWatched = NO;
static void GRTWatchDeactivate(void) {
	if (grtDeactivateWatched) {
		return;
	}
	grtDeactivateWatched = YES;
	[[NSNotificationCenter defaultCenter]
		addObserverForName:NSApplicationDidResignActiveNotification
		object:nil
		queue:[NSOperationQueue mainQueue]
		usingBlock:^(NSNotification *note) {
			grtAppResignedActive();
		}];
}
*/
import "C"

import (
	"os"
	"unsafe"
)

// placePopover positions the popover next to the menu bar and focuses it.
func placePopover(title string, width, height float32) {
	c := C.CString(title)
	defer C.free(unsafe.Pointer(c))
	C.GRTPlacePopover(c, C.double(width), C.double(height))
	if os.Getenv("GRT_VIBRANCY") == "1" {
		C.GRTApplyVibrancy(c)
	}
}

// resizePopover changes the already-open popover's size while keeping its top-left
// corner anchored (it does not re-position to the cursor).
func resizePopover(title string, width, height float32) {
	c := C.CString(title)
	defer C.free(unsafe.Pointer(c))
	C.GRTResizePopover(c, C.double(width), C.double(height))
}

// popoverAutoHide is invoked (on the main thread) when the app resigns active,
// i.e. the user clicked outside it. Set via watchPopoverAutoHide.
var popoverAutoHide func()

// watchPopoverAutoHide registers hide to run whenever the app loses active
// status, so the popover dismisses when the user clicks away.
func watchPopoverAutoHide(hide func()) {
	popoverAutoHide = hide
	C.GRTWatchDeactivate()
}
