find_package(Python3 3.8 REQUIRED COMPONENTS Interpreter)
add_test(NAME plugin_ui_compile_test
  COMMAND Python3::Interpreter -m unittest discover -s ${CMAKE_CURRENT_SOURCE_DIR} -p "test_*.py")

add_executable(aitbase_plugin_runtime_test aitbase_plugin_runtime_test.c)
target_link_libraries(aitbase_plugin_runtime_test PRIVATE aitbase_plugin)
target_include_directories(aitbase_plugin_runtime_test PRIVATE ${PROJECT_SOURCE_DIR}/src ${PROJECT_SOURCE_DIR}/third_party/cjson)
set_target_properties(aitbase_plugin_runtime_test PROPERTIES C_STANDARD 99 C_STANDARD_REQUIRED YES)
target_compile_options(aitbase_plugin_runtime_test PRIVATE -Wall -Wextra -Werror)
add_test(NAME aitbase_plugin_runtime_test COMMAND aitbase_plugin_runtime_test)

# The WASM runtime, built natively: the test provides the host imports of docs/wasm-abi.md.
add_executable(aitbase_plugin_wasm_test
  aitbase_plugin_wasm_test.c
  ${PROJECT_SOURCE_DIR}/src/aitbase_plugin_core.c
  ${PROJECT_SOURCE_DIR}/src/aitbase_plugin_wasm.c
  ${PROJECT_SOURCE_DIR}/third_party/cjson/cJSON.c
)
target_include_directories(aitbase_plugin_wasm_test PRIVATE
  ${PROJECT_SOURCE_DIR}/include ${PROJECT_SOURCE_DIR}/src ${PROJECT_SOURCE_DIR}/third_party/cjson)
target_link_libraries(aitbase_plugin_wasm_test PRIVATE m)
set_target_properties(aitbase_plugin_wasm_test PROPERTIES C_STANDARD 99 C_STANDARD_REQUIRED YES)
target_compile_options(aitbase_plugin_wasm_test PRIVATE -Wall -Wextra -Werror)
add_test(NAME aitbase_plugin_wasm_test COMMAND aitbase_plugin_wasm_test)
