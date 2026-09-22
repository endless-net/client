//go:build darwin && cgo

#include "darwin_power.h"
#include <IOKit/IOMessage.h>
#include <stdbool.h>
#include <stdatomic.h>
#include <stdlib.h>

extern void goDarwinPowerEvent(uint32_t message);

struct darwin_power_source {
    io_connect_t root;
    IONotificationPortRef port;
    io_object_t notifier;
    CFRunLoopRef loop;
    atomic_bool stopping;
};

static void darwin_power_callback(void *refcon, io_service_t service,
                                  natural_t message, void *argument) {
    darwin_power_source *source = (darwin_power_source *)refcon;
    (void)service;
    switch (message) {
    case kIOMessageCanSystemSleep:
        IOAllowPowerChange(source->root, (long)argument);
        break;
    case kIOMessageSystemWillSleep:
        // The Go callback waits for the runtime teardown or its bounded deadline.
        goDarwinPowerEvent((uint32_t)message);
        IOAllowPowerChange(source->root, (long)argument);
        break;
    case kIOMessageSystemHasPoweredOn:
        goDarwinPowerEvent((uint32_t)message);
        break;
    default:
        break;
    }
}

darwin_power_source *darwin_power_start(void) {
    darwin_power_source *source = calloc(1, sizeof(*source));
    if (source == NULL) {
        return NULL;
    }
    atomic_init(&source->stopping, false);
    source->root = IORegisterForSystemPower(source, &source->port,
                                            darwin_power_callback,
                                            &source->notifier);
    if (source->root == 0 || source->port == NULL) {
        darwin_power_destroy(source);
        return NULL;
    }
    source->loop = CFRunLoopGetCurrent();
    CFRetain(source->loop);
    CFRunLoopAddSource(source->loop, IONotificationPortGetRunLoopSource(source->port),
                       kCFRunLoopCommonModes);
    return source;
}

void darwin_power_run(darwin_power_source *source) {
    if (source != NULL) {
        // A bounded iteration also observes a stop requested before Run starts.
        while (!atomic_load(&source->stopping)) {
            CFRunLoopRunInMode(kCFRunLoopDefaultMode, 1.0, true);
        }
    }
}

void darwin_power_stop(darwin_power_source *source) {
    if (source != NULL && source->loop != NULL) {
        atomic_store(&source->stopping, true);
        CFRunLoopStop(source->loop);
        CFRunLoopWakeUp(source->loop);
    }
}

void darwin_power_destroy(darwin_power_source *source) {
    if (source == NULL) {
        return;
    }
    if (source->loop != NULL && source->port != NULL) {
        CFRunLoopRemoveSource(source->loop,
                              IONotificationPortGetRunLoopSource(source->port),
                              kCFRunLoopCommonModes);
    }
    if (source->notifier != 0) {
        IODeregisterForSystemPower(&source->notifier);
    }
    if (source->root != 0) {
        IOServiceClose(source->root);
    }
    if (source->port != NULL) {
        IONotificationPortDestroy(source->port);
    }
    if (source->loop != NULL) {
        CFRelease(source->loop);
    }
    free(source);
}
