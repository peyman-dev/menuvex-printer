//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

@interface CoffishAppDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate, WKScriptMessageHandler>
@property (nonatomic, strong) NSWindow *window;
@property (nonatomic, strong) WKWebView *webView;
@property (nonatomic, copy) NSString *initialURL;
@end

@implementation CoffishAppDelegate

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}

- (void)printCurrentPage:(id)sender {
    if (!self.webView || !self.window) return;
    NSPrintInfo *printInfo = [NSPrintInfo sharedPrintInfo];
    if ([self.webView respondsToSelector:@selector(printOperationWithPrintInfo:)]) {
        NSPrintOperation *op = [self.webView printOperationWithPrintInfo:printInfo];
        [op setShowsPrintPanel:YES];
        [op setShowsProgressPanel:YES];
        [op runOperationModalForWindow:self.window delegate:nil didRunSelector:nil contextInfo:NULL];
    }
}

- (void)reloadPage:(id)sender {
    if (self.webView) {
        [self.webView reloadFromOrigin];
    }
}

- (void)userContentController:(WKUserContentController *)userContentController
      didReceiveScriptMessage:(WKScriptMessage *)message {
    if ([[message name] isEqualToString:@"coffishPrint"]) {
        [self printCurrentPage:nil];
    }
}

- (void)setupMenuBar {
    NSMenu *menubar = [[NSMenu alloc] init];
    NSMenuItem *appMenuItem = [[NSMenuItem alloc] init];
    [menubar addItem:appMenuItem];

    NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"Coffish"];
    NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"خروج از منووکس"
                                                      action:@selector(terminate:)
                                               keyEquivalent:@"q"];
    [appMenu addItem:quitItem];
    [appMenuItem setSubmenu:appMenu];

    NSMenuItem *fileMenuItem = [[NSMenuItem alloc] init];
    [menubar addItem:fileMenuItem];
    NSMenu *fileMenu = [[NSMenu alloc] initWithTitle:@"فایل"];
    NSMenuItem *printItem = [[NSMenuItem alloc] initWithTitle:@"چاپ فیش (Print)"
                                                       action:@selector(printCurrentPage:)
                                                keyEquivalent:@"p"];
    [printItem setTarget:self];
    [fileMenu addItem:printItem];
    NSMenuItem *reloadItem = [[NSMenuItem alloc] initWithTitle:@"بارگذاری مجدد"
                                                        action:@selector(reloadPage:)
                                                 keyEquivalent:@"r"];
    [reloadItem setTarget:self];
    [fileMenu addItem:reloadItem];
    [fileMenuItem setSubmenu:fileMenu];

    NSMenuItem *editMenuItem = [[NSMenuItem alloc] init];
    [menubar addItem:editMenuItem];
    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"ویرایش"];
    [editMenu addItemWithTitle:@"واگرد (Undo)" action:@selector(undo:) keyEquivalent:@"z"];
    [editMenu addItemWithTitle:@"از نو (Redo)" action:@selector(redo:) keyEquivalent:@"Z"];
    [editMenu addItem:[NSMenuItem separatorItem]];
    [editMenu addItemWithTitle:@"برش (Cut)" action:@selector(cut:) keyEquivalent:@"x"];
    [editMenu addItemWithTitle:@"کپی (Copy)" action:@selector(copy:) keyEquivalent:@"c"];
    [editMenu addItemWithTitle:@"چسباندن (Paste)" action:@selector(paste:) keyEquivalent:@"v"];
    [editMenu addItemWithTitle:@"انتخاب همه (Select All)" action:@selector(selectAll:) keyEquivalent:@"a"];
    [editMenuItem setSubmenu:editMenu];

    [NSApp setMainMenu:menubar];
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    [self setupMenuBar];

    NSRect frame = NSMakeRect(0, 0, 1400, 880);
    NSUInteger style = NSWindowStyleMaskTitled |
                       NSWindowStyleMaskClosable |
                       NSWindowStyleMaskMiniaturizable |
                       NSWindowStyleMaskResizable;

    self.window = [[NSWindow alloc] initWithContentRect:frame
                                              styleMask:style
                                                backing:NSBackingStoreBuffered
                                                  defer:NO];
    [self.window setTitle:@"منووکس — سامانه جامع مدیریت کافه و رستوران"];
    [self.window setMinSize:NSMakeSize(1024, 680)];
    [self.window setDelegate:self];
    [self.window center];

    WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
    [[config userContentController] addScriptMessageHandler:self name:@"coffishPrint"];

    self.webView = [[WKWebView alloc] initWithFrame:[[self.window contentView] bounds]
                                      configuration:config];
    [self.webView setAutoresizingMask:(NSViewWidthSizable | NSViewHeightSizable)];
    [[self.window contentView] addSubview:self.webView];

    NSURL *nsurl = [NSURL URLWithString:self.initialURL];
    NSURLRequest *req = [NSURLRequest requestWithURL:nsurl
                                         cachePolicy:NSURLRequestReloadIgnoringLocalCacheData
                                     timeoutInterval:30.0];
    [self.webView loadRequest:req];

    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}

@end

static void launchNativeCocoaWindow(const char *urlCStr) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyRegular];

        CoffishAppDelegate *delegate = [[CoffishAppDelegate alloc] init];
        delegate.initialURL = [NSString stringWithUTF8String:urlCStr];
        [app setDelegate:delegate];

        [app run];
    }
}
*/
import "C"
import "unsafe"

// openDesktopWindow shows the embedded WKWebView window and blocks until the
// user closes it, so the caller can stop the local backend afterwards.
func openDesktopWindow(url string) bool {
	cstr := C.CString(url)
	defer C.free(unsafe.Pointer(cstr))
	C.launchNativeCocoaWindow(cstr)
	return true
}
