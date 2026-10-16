cmake_minimum_required(VERSION 3.13)
project(aitbase_vision_plugin_sdk VERSION 0.4.0 LANGUAGES C)

# Usable standalone (tests) or as a subdirectory/submodule of a plugin project.
option(AITBASE_PLUGIN_SDK_TESTS "Build the SDK tests" ${PROJECT_IS_TOP_LEVEL})

include(${CMAKE_CURRENT_SOURCE_DIR}/cmake/AitbasePluginUi.cmake)

# Plugins link this and implement the callbacks in aitbase_plugin.h. Native builds get main() and
# the development runtime; WASM builds (wasi-sdk, CMAKE_SYSTEM_NAME WASI) get the guest half of the
# device's WASM host ABI (docs/wasm-abi.md) and produce <entry>.wasm.
if(CMAKE_SYSTEM_NAME STREQUAL "WASI")
  set(AITBASE_PLUGIN_RUNTIME_SOURCES src/aitbase_plugin_wasm.c)
else()
  set(AITBASE_PLUGIN_RUNTIME_SOURCES src/aitbase_plugin_dev.c src/aitbase_plugin_main.c)
endif()
add_library(aitbase_plugin STATIC
  src/aitbase_plugin_core.c
  src/aitbase_plugin_defaults.c
  ${AITBASE_PLUGIN_RUNTIME_SOURCES}
  third_party/cjson/cJSON.c
)
add_library(aitbase::plugin ALIAS aitbase_plugin)
target_include_directories(aitbase_plugin
  PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include
  PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/src ${CMAKE_CURRENT_SOURCE_DIR}/third_party/cjson
)
set_target_properties(aitbase_plugin PROPERTIES C_STANDARD 99 C_STANDARD_REQUIRED YES)
target_compile_options(aitbase_plugin PRIVATE -Wall -Wextra)
# cJSON needs isnan/isinf, which uClibc keeps in libm.
target_link_libraries(aitbase_plugin PUBLIC m)
if(CMAKE_SYSTEM_NAME STREQUAL "WASI")
  # A reactor has no main(): the device's runner calls the plugin's exports.
  target_link_options(aitbase_plugin INTERFACE -mexec-model=reactor)
endif()

if(AITBASE_PLUGIN_SDK_TESTS AND NOT CMAKE_SYSTEM_NAME STREQUAL "WASI")
  enable_testing()
  add_subdirectory(tests)
endif()
