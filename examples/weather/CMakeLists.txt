cmake_minimum_required(VERSION 3.13)
project(weather LANGUAGES C)

# sdk/ is the SDK copy that `aitbase-plugin init` creates; copy this example into such a project.
add_subdirectory(sdk)

add_executable(weather src/main.c)  # no main(): the SDK provides it
target_link_libraries(weather PRIVATE aitbase::plugin)
set_target_properties(weather PROPERTIES C_STANDARD 99 C_STANDARD_REQUIRED YES)
target_compile_options(weather PRIVATE -Wall -Wextra -Werror)
