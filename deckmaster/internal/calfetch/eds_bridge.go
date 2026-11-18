package calfetch

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

typedef int gboolean;
typedef void* gpointer;
typedef const void* gconstpointer;
typedef void (*GDestroyNotify)(gpointer data);

typedef struct _GList {
    gpointer data;
    struct _GList *next;
    struct _GList *prev;
} GList;

typedef struct _GSList {
    gpointer data;
    struct _GSList *next;
} GSList;

typedef struct _GError {
    uint32_t domain;
    int code;
    char *message;
} GError;

typedef struct {
    char *summary;
    char *location;
    int sy, sm, sd, sh, smin, ss, sis_utc;
    int ey, em, ed, eh, emin, es, eis_utc;
} CalEventC;

typedef struct {
    CalEventC *events;
    size_t count;
    size_t cap;
} CalCtxC;

// GLib / GObject
static void (*fn_g_object_unref)(gpointer obj) = NULL;
static void (*fn_g_list_free_full)(GList *list, GDestroyNotify free_func) = NULL;
static void (*fn_g_slist_free_full)(GSList *list, GDestroyNotify free_func) = NULL;
static void (*fn_g_error_free)(GError *err) = NULL;

// EDataServer
static gpointer (*fn_e_source_registry_new_sync)(gpointer cancellable, GError **error) = NULL;
static GList* (*fn_e_source_registry_list_sources)(gpointer registry, const char *extension_name) = NULL;

// ECal
static gpointer (*fn_e_cal_client_connect_sync)(gpointer source, int source_type, uint32_t wait_for_connected_seconds, gpointer cancellable, GError **error) = NULL;
static gboolean (*fn_e_cal_client_get_object_list_as_comps_sync)(gpointer client, const char *sexp, GSList **out_ecalcomps, gpointer cancellable, GError **error) = NULL;
static void (*fn_e_cal_client_generate_instances_for_object_sync)(gpointer client, gpointer icalcomp, time_t start, time_t end, gpointer cancellable, gpointer cb, gpointer cb_data) = NULL;
static gpointer (*fn_e_cal_component_get_icalcomponent)(gpointer comp) = NULL;

// ICalGLib
static gpointer (*fn_i_cal_component_get_first_property)(gpointer comp, int kind) = NULL;
static const char* (*fn_i_cal_component_get_summary)(gpointer comp) = NULL;
static const char* (*fn_i_cal_component_get_location)(gpointer comp) = NULL;
static gpointer (*fn_i_cal_property_get_first_parameter)(gpointer prop, int kind) = NULL;
static const char* (*fn_i_cal_parameter_get_tzid)(gpointer param) = NULL;
static int (*fn_i_cal_time_get_year)(gpointer tt) = NULL;
static int (*fn_i_cal_time_get_month)(gpointer tt) = NULL;
static int (*fn_i_cal_time_get_day)(gpointer tt) = NULL;
static int (*fn_i_cal_time_get_hour)(gpointer tt) = NULL;
static int (*fn_i_cal_time_get_minute)(gpointer tt) = NULL;
static int (*fn_i_cal_time_get_second)(gpointer tt) = NULL;
static gboolean (*fn_i_cal_time_is_utc)(gpointer tt) = NULL;
static gboolean (*fn_i_cal_time_is_date)(gpointer tt) = NULL;

static int cal_eds_loaded = 0;

static int cal_eds_init(void) {
    if (cal_eds_loaded) return 1;

    void *libglib = dlopen("libglib-2.0.so.0", RTLD_LAZY | RTLD_LOCAL);
    void *libgobj = dlopen("libgobject-2.0.so.0", RTLD_LAZY | RTLD_LOCAL);
    void *libeds  = dlopen("libedataserver-1.2.so.27", RTLD_LAZY | RTLD_LOCAL);
    void *libecal = dlopen("libecal-2.0.so.3", RTLD_LAZY | RTLD_LOCAL);
    void *libical = dlopen("libical-glib.so.3", RTLD_LAZY | RTLD_LOCAL);

    if (!libglib || !libgobj || !libeds || !libecal || !libical) {
        return 0;
    }

    fn_g_object_unref = dlsym(libgobj, "g_object_unref");
    fn_g_list_free_full = dlsym(libglib, "g_list_free_full");
    fn_g_slist_free_full = dlsym(libglib, "g_slist_free_full");
    fn_g_error_free = dlsym(libglib, "g_error_free");

    fn_e_source_registry_new_sync = dlsym(libeds, "e_source_registry_new_sync");
    fn_e_source_registry_list_sources = dlsym(libeds, "e_source_registry_list_sources");

    fn_e_cal_client_connect_sync = dlsym(libecal, "e_cal_client_connect_sync");
    fn_e_cal_client_get_object_list_as_comps_sync = dlsym(libecal, "e_cal_client_get_object_list_as_comps_sync");
    fn_e_cal_client_generate_instances_for_object_sync = dlsym(libecal, "e_cal_client_generate_instances_for_object_sync");
    fn_e_cal_component_get_icalcomponent = dlsym(libecal, "e_cal_component_get_icalcomponent");

    fn_i_cal_component_get_first_property = dlsym(libical, "i_cal_component_get_first_property");
    fn_i_cal_component_get_summary = dlsym(libical, "i_cal_component_get_summary");
    fn_i_cal_component_get_location = dlsym(libical, "i_cal_component_get_location");
    fn_i_cal_property_get_first_parameter = dlsym(libical, "i_cal_property_get_first_parameter");
    fn_i_cal_parameter_get_tzid = dlsym(libical, "i_cal_parameter_get_tzid");
    fn_i_cal_time_get_year = dlsym(libical, "i_cal_time_get_year");
    fn_i_cal_time_get_month = dlsym(libical, "i_cal_time_get_month");
    fn_i_cal_time_get_day = dlsym(libical, "i_cal_time_get_day");
    fn_i_cal_time_get_hour = dlsym(libical, "i_cal_time_get_hour");
    fn_i_cal_time_get_minute = dlsym(libical, "i_cal_time_get_minute");
    fn_i_cal_time_get_second = dlsym(libical, "i_cal_time_get_second");
    fn_i_cal_time_is_utc = dlsym(libical, "i_cal_time_is_utc");
    fn_i_cal_time_is_date = dlsym(libical, "i_cal_time_is_date");

    if (!fn_g_object_unref || !fn_g_list_free_full || !fn_g_slist_free_full || !fn_g_error_free ||
        !fn_e_source_registry_new_sync || !fn_e_source_registry_list_sources ||
        !fn_e_cal_client_connect_sync || !fn_e_cal_client_get_object_list_as_comps_sync ||
        !fn_e_cal_client_generate_instances_for_object_sync || !fn_e_cal_component_get_icalcomponent ||
        !fn_i_cal_component_get_first_property || !fn_i_cal_component_get_summary ||
        !fn_i_cal_component_get_location || !fn_i_cal_property_get_first_parameter ||
        !fn_i_cal_parameter_get_tzid || !fn_i_cal_time_get_year || !fn_i_cal_time_get_month ||
        !fn_i_cal_time_get_day || !fn_i_cal_time_get_hour || !fn_i_cal_time_get_minute ||
        !fn_i_cal_time_get_second || !fn_i_cal_time_is_utc || !fn_i_cal_time_is_date) {
        return 0;
    }

    cal_eds_loaded = 1;
    return 1;
}

static gboolean cal_instance_cb(gpointer icomp, gpointer instance_start, gpointer instance_end,
                                gpointer user_data, gpointer cancellable, GError **error) {
    (void)cancellable;
    (void)error;
    if (!instance_start || fn_i_cal_time_is_date(instance_start)) {
        return 1;
    }

    CalCtxC *ctx = (CalCtxC *)user_data;
    if (ctx->count == ctx->cap) {
        size_t new_cap = ctx->cap == 0 ? 16 : ctx->cap * 2;
        CalEventC *next = (CalEventC *)realloc(ctx->events, new_cap * sizeof(CalEventC));
        if (!next) return 1;
        ctx->events = next;
        ctx->cap = new_cap;
    }

    const char *sum = icomp ? fn_i_cal_component_get_summary(icomp) : NULL;
    const char *loc = icomp ? fn_i_cal_component_get_location(icomp) : NULL;

    CalEventC *ev = &ctx->events[ctx->count++];
    ev->summary = strdup(sum && sum[0] ? sum : "?");
    ev->location = strdup(loc ? loc : "");

    ev->sy = fn_i_cal_time_get_year(instance_start);
    ev->sm = fn_i_cal_time_get_month(instance_start);
    ev->sd = fn_i_cal_time_get_day(instance_start);
    ev->sh = fn_i_cal_time_get_hour(instance_start);
    ev->smin = fn_i_cal_time_get_minute(instance_start);
    ev->ss = fn_i_cal_time_get_second(instance_start);
    ev->sis_utc = fn_i_cal_time_is_utc(instance_start) ? 1 : 0;

    ev->ey = fn_i_cal_time_get_year(instance_end);
    ev->em = fn_i_cal_time_get_month(instance_end);
    ev->ed = fn_i_cal_time_get_day(instance_end);
    ev->eh = fn_i_cal_time_get_hour(instance_end);
    ev->emin = fn_i_cal_time_get_minute(instance_end);
    ev->es = fn_i_cal_time_get_second(instance_end);
    ev->eis_utc = fn_i_cal_time_is_utc(instance_end) ? 1 : 0;

    return 1;
}

static char* cal_get_prop_tzid(gpointer ical, int prop_kind) {
    if (!ical) return NULL;
    gpointer prop = fn_i_cal_component_get_first_property(ical, prop_kind);
    if (!prop) return NULL;
    gpointer param = fn_i_cal_property_get_first_parameter(prop, 27); // I_CAL_TZID_PARAMETER = 27
    char *res = NULL;
    if (param) {
        const char *tz = fn_i_cal_parameter_get_tzid(param);
        if (tz && tz[0]) res = strdup(tz);
        fn_g_object_unref(param);
    }
    fn_g_object_unref(prop);
    return res;
}

static gpointer* cal_connect_sources(int *out_count) {
    *out_count = 0;
    GError *err = NULL;
    gpointer registry = fn_e_source_registry_new_sync(NULL, &err);
    if (err) {
        fn_g_error_free(err);
        err = NULL;
    }
    if (!registry) return NULL;

    GList *sources = fn_e_source_registry_list_sources(registry, "Calendar");
    int cap = 0;
    for (GList *l = sources; l != NULL; l = l->next) cap++;

    gpointer *clients = cap > 0 ? (gpointer *)malloc(cap * sizeof(gpointer)) : NULL;
    int n = 0;
    for (GList *l = sources; l != NULL && clients != NULL; l = l->next) {
        GError *cerr = NULL;
        // E_CAL_CLIENT_SOURCE_TYPE_EVENTS = 0, wait_for_connected_seconds = 10
        gpointer client = fn_e_cal_client_connect_sync(l->data, 0, 10, NULL, &cerr);
        if (cerr) {
            fn_g_error_free(cerr);
            cerr = NULL;
        }
        if (client) {
            clients[n++] = client;
        }
    }

    if (sources) fn_g_list_free_full(sources, fn_g_object_unref);
    fn_g_object_unref(registry);

    *out_count = n;
    return clients;
}

static void cal_free_clients(gpointer *clients, int count) {
    if (!clients) return;
    for (int i = 0; i < count; i++) {
        if (clients[i]) fn_g_object_unref(clients[i]);
    }
    free(clients);
}

static void cal_query_client(gpointer client, const char *sexp, int64_t start_ts, int64_t end_ts,
                             CalCtxC *ctx, char ***out_start_tzids, char ***out_end_tzids) {
    GSList *comps = NULL;
    GError *err = NULL;
    gboolean ok = fn_e_cal_client_get_object_list_as_comps_sync(client, sexp, &comps, NULL, &err);
    if (err) {
        fn_g_error_free(err);
        err = NULL;
    }
    if (!ok || !comps) {
        if (comps) fn_g_slist_free_full(comps, fn_g_object_unref);
        return;
    }

    for (GSList *l = comps; l != NULL; l = l->next) {
        gpointer comp = l->data;
        gpointer ical = fn_e_cal_component_get_icalcomponent(comp);
        if (!ical) continue;

        char *stz = cal_get_prop_tzid(ical, 32); // I_CAL_DTSTART_PROPERTY = 32
        char *etz = cal_get_prop_tzid(ical, 30); // I_CAL_DTEND_PROPERTY = 30

        size_t prev_count = ctx->count;
        fn_e_cal_client_generate_instances_for_object_sync(
            client, ical, (time_t)start_ts, (time_t)end_ts, NULL, (gpointer)cal_instance_cb, ctx
        );

        if (ctx->count > prev_count) {
            *out_start_tzids = (char **)realloc(*out_start_tzids, ctx->count * sizeof(char *));
            *out_end_tzids = (char **)realloc(*out_end_tzids, ctx->count * sizeof(char *));
            for (size_t i = prev_count; i < ctx->count; i++) {
                (*out_start_tzids)[i] = stz ? strdup(stz) : NULL;
                (*out_end_tzids)[i] = etz ? strdup(etz) : NULL;
            }
        }
        if (stz) free(stz);
        if (etz) free(etz);
    }

    fn_g_slist_free_full(comps, fn_g_object_unref);
}

static void cal_free_ctx(CalCtxC *ctx, char **start_tzids, char **end_tzids) {
    for (size_t i = 0; i < ctx->count; i++) {
        free(ctx->events[i].summary);
        free(ctx->events[i].location);
        if (start_tzids && start_tzids[i]) free(start_tzids[i]);
        if (end_tzids && end_tzids[i]) free(end_tzids[i]);
    }
    free(ctx->events);
    if (start_tzids) free(start_tzids);
    if (end_tzids) free(end_tzids);
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

type edsClients struct {
	ptr   *C.gpointer
	count int
}

func connectEDSSources() (*edsClients, error) {
	if C.cal_eds_init() == 0 {
		return nil, errors.New("failed to load GNOME EDS shared libraries")
	}
	var count C.int
	ptr := C.cal_connect_sources(&count)
	return &edsClients{ptr: ptr, count: int(count)}, nil
}

func (c *edsClients) close() {
	if c != nil && c.ptr != nil {
		C.cal_free_clients(c.ptr, C.int(c.count))
		c.ptr = nil
		c.count = 0
	}
}

type rawEDSInstance struct {
	summary  string
	location string
	startTZ  string
	endTZ    string
	sy, sm, sd, sh, smin, ss int
	sisUTC                   bool
	ey, em, ed, eh, emin, es int
	eisUTC                   bool
}

func (c *edsClients) queryRange(sexp string, startTS, endTS int64) []rawEDSInstance {
	if c == nil || c.ptr == nil || c.count == 0 {
		return nil
	}
	cSexp := C.CString(sexp)
	defer C.free(unsafe.Pointer(cSexp))

	var ctx C.CalCtxC
	var startTZs **C.char
	var endTZs **C.char
	defer func() {
		C.cal_free_ctx(&ctx, startTZs, endTZs)
	}()

	clientSlice := unsafe.Slice(c.ptr, c.count)
	for _, cl := range clientSlice {
		C.cal_query_client(cl, cSexp, C.int64_t(startTS), C.int64_t(endTS), &ctx, &startTZs, &endTZs)
	}

	if ctx.count == 0 {
		return nil
	}

	out := make([]rawEDSInstance, int(ctx.count))
	evSlice := unsafe.Slice(ctx.events, int(ctx.count))
	stzSlice := unsafe.Slice(startTZs, int(ctx.count))
	etzSlice := unsafe.Slice(endTZs, int(ctx.count))

	for i := 0; i < int(ctx.count); i++ {
		ev := evSlice[i]
		var stz, etz string
		if stzSlice[i] != nil {
			stz = C.GoString(stzSlice[i])
		}
		if etzSlice[i] != nil {
			etz = C.GoString(etzSlice[i])
		}
		out[i] = rawEDSInstance{
			summary:  C.GoString(ev.summary),
			location: C.GoString(ev.location),
			startTZ:  stz,
			endTZ:    etz,
			sy:       int(ev.sy),
			sm:       int(ev.sm),
			sd:       int(ev.sd),
			sh:       int(ev.sh),
			smin:     int(ev.smin),
			ss:       int(ev.ss),
			sisUTC:   ev.sis_utc != 0,
			ey:       int(ev.ey),
			em:       int(ev.em),
			ed:       int(ev.ed),
			eh:       int(ev.eh),
			emin:     int(ev.emin),
			es:       int(ev.es),
			eisUTC:   ev.eis_utc != 0,
		}
	}
	return out
}
