cmake_minimum_required(VERSION 3.13)
project(__TARGET__ LANGUAGES C)

# sdk/ is the SDK copy that `aitbase-plugin init` created (version in sdk/VERSION).
add_subdirectory(sdk)

add_executable(__TARGET__ src/main.c)  # no main(): the SDK provides it
target_link_libraries(__TARGET__ PRIVATE aitbase::plugin)
set_target_properties(__TARGET__ PROPERTIES C_STANDARD 99 C_STANDARD_REQUIRED YES)
target_compile_options(__TARGET__ PRIVATE -Wall -Wextra -Werror)

# Keep SCREENS the same as "ui.screens" in plugin.json.
aitbase_plugin_add_ui(__TARGET__ SCREENS main)
