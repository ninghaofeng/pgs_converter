#import <AppKit/AppKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#include <stdint.h>

extern void pgs_picker_done(uintptr_t handle, char *result, char *diagnostic);

// Accessed only on AppKit's main queue, including cancellation after completion.
static NSMutableDictionary<NSNumber *, NSOpenPanel *> *panels;

static void present_panel(NSWindow *window, NSString *prompt, int directory, uintptr_t handle) {
    @autoreleasepool {
        @try {
            if (!window.isVisible) {
                pgs_picker_done(handle, NULL, "native macOS window is not visible");
                return;
            }
            if (panels == nil) {
                panels = [[NSMutableDictionary alloc] init];
            }
            NSOpenPanel *panel = [NSOpenPanel openPanel];
            panel.title = prompt;
            panel.canChooseFiles = !directory;
            panel.canChooseDirectories = directory;
            panel.allowsMultipleSelection = !directory;
            panel.canCreateDirectories = directory;
            panel.resolvesAliases = YES;
            panel.treatsFilePackagesAsDirectories = NO;
            if (!directory) {
                panel.allowedContentTypes = @[
                    [UTType typeWithFilenameExtension:@"srt"],
                    [UTType typeWithFilenameExtension:@"ass"],
                    [UTType typeWithFilenameExtension:@"ssa"]
                ];
                panel.allowsOtherFileTypes = NO;
            }
            NSNumber *key = @(handle);
            panels[key] = panel;
            [panel beginSheetModalForWindow:window completionHandler:^(NSModalResponse response) {
                @autoreleasepool {
                    if (response != NSModalResponseOK) {
                        pgs_picker_done(handle, NULL, NULL);
                    } else {
                        NSMutableArray<NSString *> *paths = [NSMutableArray array];
                        for (NSURL *url in panel.URLs) {
                            [paths addObject:url.path];
                        }
                        NSError *error = nil;
                        NSData *data = [NSJSONSerialization dataWithJSONObject:paths options:0 error:&error];
                        if (data == nil) {
                            pgs_picker_done(handle, NULL, (char *)error.localizedDescription.UTF8String);
                        } else {
                            NSMutableData *terminated = [NSMutableData dataWithData:data];
                            [terminated appendBytes:"" length:1];
                            pgs_picker_done(handle, (char *)terminated.bytes, NULL);
                        }
                    }
                    [panels removeObjectForKey:key];
                }
            }];
        } @catch (NSException *exception) {
            [panels removeObjectForKey:@(handle)];
            pgs_picker_done(handle, NULL, (char *)exception.description.UTF8String);
        }
    }
}

void pgs_picker_open(uintptr_t parent, const char *title, int directory, uintptr_t handle) {
    @autoreleasepool {
        // Copy the title before Go frees its temporary C string on return.
        NSString *prompt = [NSString stringWithUTF8String:title];
        NSWindow *window = (NSWindow *)parent;
        // Fyne's native handle callback does not guarantee AppKit's main thread.
        dispatch_async(dispatch_get_main_queue(), ^{
            present_panel(window, prompt, directory, handle);
        });
    }
}

void pgs_picker_cancel(uintptr_t handle) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [panels[@(handle)] cancel:nil];
    });
}
