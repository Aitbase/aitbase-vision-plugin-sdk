/*
 * Defaults for optional plugin callbacks.
 *
 * This file is its own object in the static library, so the linker only pulls it in when the
 * plugin does not define the callback itself. That works on every linker; a weak *declaration*
 * does not, because Apple's ld rejects an unresolved weak reference. The definition is also weak,
 * so it never clashes with a plugin's own.
 */
#include "aitbase_plugin.h"

#if defined(__GNUC__)
__attribute__((weak))
#endif
void aitbase_on_stop(aitbase_plugin_t *plugin) {
    (void)plugin;
}
