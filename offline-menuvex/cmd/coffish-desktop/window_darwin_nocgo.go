//go:build darwin && !cgo

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

const objcNativeHostSource = `#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

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
    if (self.webView) [self.webView reloadFromOrigin];
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
    [appMenu addItemWithTitle:@"خروج از منووکس" action:@selector(terminate:) keyEquivalent:@"q"];
    [appMenuItem setSubmenu:appMenu];

    NSMenuItem *fileMenuItem = [[NSMenuItem alloc] init];
    [menubar addItem:fileMenuItem];
    NSMenu *fileMenu = [[NSMenu alloc] initWithTitle:@"فایل"];
    NSMenuItem *printItem = [[NSMenuItem alloc] initWithTitle:@"چاپ فیش (Print)" action:@selector(printCurrentPage:) keyEquivalent:@"p"];
    [printItem setTarget:self];
    [fileMenu addItem:printItem];
    NSMenuItem *reloadItem = [[NSMenuItem alloc] initWithTitle:@"بارگذاری مجدد" action:@selector(reloadPage:) keyEquivalent:@"r"];
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
    NSUInteger style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable;
    self.window = [[NSWindow alloc] initWithContentRect:frame styleMask:style backing:NSBackingStoreBuffered defer:NO];
    [self.window setTitle:@"منووکس — سامانه جامع مدیریت کافه و رستوران"];
    [self.window setMinSize:NSMakeSize(1024, 680)];
    [self.window setDelegate:self];
    [self.window center];

    WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
    [[config userContentController] addScriptMessageHandler:self name:@"coffishPrint"];
    self.webView = [[WKWebView alloc] initWithFrame:[[self.window contentView] bounds] configuration:config];
    [self.webView setAutoresizingMask:(NSViewWidthSizable | NSViewHeightSizable)];
    [[self.window contentView] addSubview:self.webView];

    NSURL *nsurl = [NSURL URLWithString:self.initialURL];
    NSURLRequest *req = [NSURLRequest requestWithURL:nsurl cachePolicy:NSURLRequestReloadIgnoringLocalCacheData timeoutInterval:30.0];
    [self.webView loadRequest:req];
    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}
@end

int main(int argc, const char * argv[]) {
    @autoreleasepool {
        NSString *url = @"http://127.0.0.1:8787";
        if (argc > 1) {
            url = [NSString stringWithUTF8String:argv[1]];
        }
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyRegular];
        CoffishAppDelegate *delegate = [[CoffishAppDelegate alloc] init];
        delegate.initialURL = url;
        [app setDelegate:delegate];
        [app run];
    }
    return 0;
}
`

func runNativeDesktopWindow(url string) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	hostDir := filepath.Join(cacheDir, "CoffishDesktopHost")
	_ = os.MkdirAll(hostDir, 0o755)
	srcPath := filepath.Join(hostDir, "coffish_host.m")
	binPath := filepath.Join(hostDir, "CoffishNativeWindow")

	if _, statErr := os.Stat(binPath); statErr != nil {
		_ = os.WriteFile(srcPath, []byte(objcNativeHostSource), 0o600)
		_ = exec.Command("/usr/bin/clang", "-fobjc-arc", "-framework", "Cocoa", "-framework", "WebKit", "-O2", "-o", binPath, srcPath).Run()
	}
	if _, statErr := os.Stat(binPath); statErr == nil {
		cmd := exec.Command(binPath, url)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		return
	}

	// Fallback using macOS built-in Objective-C bridge in /usr/bin/osascript (JXA Cocoa + WebKit, zero browser)
	jxa := `ObjC.import('Cocoa');
ObjC.import('WebKit');
var app = $.NSApplication.sharedApplication;
app.setActivationPolicy($.NSApplicationActivationPolicyRegular);
var frame = $.NSMakeRect(0, 0, 1400, 880);
var style = $.NSWindowStyleMaskTitled | $.NSWindowStyleMaskClosable | $.NSWindowStyleMaskMiniaturizable | $.NSWindowStyleMaskResizable;
var win = $.NSWindow.alloc.initWithContentRectStyleMaskBackingDefer(frame, style, $.NSBackingStoreBuffered, false);
win.title = 'منووکس — سامانه جامع مدیریت کافه و رستوران';
win.center;
var config = $.WKWebViewConfiguration.alloc.init;
var webView = $.WKWebView.alloc.initWithFrameConfiguration(win.contentView.bounds, config);
webView.setAutoresizingMask($.NSViewWidthSizable | $.NSViewHeightSizable);
win.contentView.addSubview(webView);
var nsurl = $.NSURL.URLWithString('` + url + `');
webView.loadRequest($.NSURLRequest.requestWithURL(nsurl));
win.makeKeyAndOrderFront(null);
app.activateIgnoringOtherApps(true);
app.run;`
	_ = exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", jxa).Run()
}
