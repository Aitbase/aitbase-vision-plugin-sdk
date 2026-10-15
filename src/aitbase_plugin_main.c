/* Process entry point for native development builds. Plugins implement callbacks, not main(). */
#include "aitbase_plugin_runtime.h"

int main(int argc, char **argv) {
    return aitbase_plugin_run(argc, argv);
}
