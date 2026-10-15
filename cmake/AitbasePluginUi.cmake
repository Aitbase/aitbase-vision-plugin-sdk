# aitbase_plugin_add_ui(<target> XML <ui.xml> [PACKAGE_DIR <dir>] OUTPUT <var>)
#
# Compiles plugin UI XML into the aitbase.plugin-ui JSON the device loads, at build time, and
# sets <var> to the generated file. PACKAGE_DIR is the directory that asset paths in the XML are
# relative to (defaults to the XML's directory).
set(AITBASE_PLUGIN_SDK_DIR ${CMAKE_CURRENT_LIST_DIR}/.. CACHE INTERNAL "")

function(aitbase_plugin_add_ui target)
  cmake_parse_arguments(ARG "" "XML;PACKAGE_DIR;OUTPUT" "" ${ARGN})
  if(NOT ARG_XML OR NOT ARG_OUTPUT)
    message(FATAL_ERROR "aitbase_plugin_add_ui: XML and OUTPUT are required")
  endif()
  find_package(Python3 3.8 REQUIRED COMPONENTS Interpreter)
  get_filename_component(xml ${ARG_XML} ABSOLUTE)
  if(ARG_PACKAGE_DIR)
    get_filename_component(package_dir ${ARG_PACKAGE_DIR} ABSOLUTE)
  else()
    get_filename_component(package_dir ${xml} DIRECTORY)
  endif()
  file(GLOB_RECURSE assets CONFIGURE_DEPENDS ${package_dir}/assets/*)
  set(out ${CMAKE_CURRENT_BINARY_DIR}/${target}_ui.json)
  add_custom_command(
    OUTPUT ${out}
    COMMAND Python3::Interpreter -I ${AITBASE_PLUGIN_SDK_DIR}/tools/plugin_ui_compile.py
            ${xml} ${package_dir} ${out}
    DEPENDS ${xml} ${assets}
            ${AITBASE_PLUGIN_SDK_DIR}/tools/plugin_ui_compile.py
            ${AITBASE_PLUGIN_SDK_DIR}/tools/plugin_ui_registry.py
            ${AITBASE_PLUGIN_SDK_DIR}/schema/plugin_ui_registry.json
    COMMENT "Compiling plugin UI ${xml}"
    VERBATIM
  )
  add_custom_target(${target}_ui ALL DEPENDS ${out})
  if(TARGET ${target})
    add_dependencies(${target} ${target}_ui)
  endif()
  set(${ARG_OUTPUT} ${out} PARENT_SCOPE)
endfunction()
