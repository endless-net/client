#ifndef ENDLESSNET_DARWIN_LOGOFF_H
#define ENDLESSNET_DARWIN_LOGOFF_H

typedef struct darwin_logoff_source darwin_logoff_source;

// Returns zero only after Endpoint Security has accepted the subscription.
int darwin_logoff_start(darwin_logoff_source **source);
void darwin_logoff_stop(darwin_logoff_source *source);

#endif
