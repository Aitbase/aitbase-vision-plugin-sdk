cmake_minimum_required(VERSION 3.13)
project(prefs LANGUAGES C)

# sdk/ is the SDK copy that `aitbase-plugin init` creates; copy this example into such a project.
add_subdirectory(sdk)

add_executable(prefs src/main.c)  # no main(): the SDK provides it
target_link_libraries(prefs PRIVATE aitbase::plugin)
set_target_properties(prefs PROPERTIES C_STANDARD 99 C_STANDARD_REQUIRED YES)
target_compile_options(prefs PRIVATE -Wall -Wextra -Werror)

aitbase_plugin_add_ui(prefs SCREENS main)
