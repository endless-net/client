//go:build darwin && cgo

#include "darwin_logoff.h"
#include <EndpointSecurity/EndpointSecurity.h>
#include <stdint.h>
#include <stdlib.h>

extern void goDarwinLogoffEvent(const char *username, size_t length,
                               uint64_t session, uint64_t sequence);
extern void goDarwinLogoffFailure(void);

struct darwin_logoff_source {
    es_client_t *client;
};

int darwin_logoff_start(darwin_logoff_source **out) {
    *out = NULL;
    darwin_logoff_source *source = calloc(1, sizeof(*source));
    if (source == NULL) {
        return -1;
    }
    es_new_client_result_t result = es_new_client(&source->client,
        ^(es_client_t *client, const es_message_t *message) {
            (void)client;
            if (message->event_type != ES_EVENT_TYPE_NOTIFY_LW_SESSION_LOGOUT ||
                message->version <= 2 || message->event.lw_session_logout == NULL) {
                goDarwinLogoffFailure();
                return;
            }
            const es_event_lw_session_logout_t *logout = message->event.lw_session_logout;
            goDarwinLogoffEvent(logout->username.data, logout->username.length,
                               logout->graphical_session_id, message->seq_num);
        });
    if (result != ES_NEW_CLIENT_RESULT_SUCCESS) {
        free(source);
        return (int)result;
    }
    es_event_type_t type = ES_EVENT_TYPE_NOTIFY_LW_SESSION_LOGOUT;
    if (es_subscribe(source->client, &type, 1) != ES_RETURN_SUCCESS) {
        es_delete_client(source->client);
        free(source);
        return -2;
    }
    *out = source;
    return 0;
}

void darwin_logoff_stop(darwin_logoff_source *source) {
    if (source != NULL) {
        es_delete_client(source->client);
        free(source);
    }
}
