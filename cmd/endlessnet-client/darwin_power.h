#ifndef ENDLESSNET_DARWIN_POWER_H
#define ENDLESSNET_DARWIN_POWER_H

#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <stdint.h>

typedef struct darwin_power_source darwin_power_source;

darwin_power_source *darwin_power_start(void);
void darwin_power_run(darwin_power_source *source);
void darwin_power_stop(darwin_power_source *source);
void darwin_power_destroy(darwin_power_source *source);

#endif
