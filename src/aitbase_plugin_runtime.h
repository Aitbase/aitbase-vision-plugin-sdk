#ifndef AITBASE_PLUGIN_RUNTIME_H
#define AITBASE_PLUGIN_RUNTIME_H

/* SDK-internal: runs the plugin's callbacks until it stops. Returns the process exit status.
 * The SDK provides main() (aitbase_plugin_main.c); tests call this directly. */
int aitbase_plugin_run(int argc, char **argv);

#endif /* AITBASE_PLUGIN_RUNTIME_H */
