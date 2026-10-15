# aitbase_plugin_add_ui(<target> [UI_DIR <dir>] SCREENS <screen>...)
#
# Compiles each <UI_DIR>/screens/<screen>.xml (with UI_DIR/components/ and UI_DIR/globals.xml)
# into <screen>.ui.json next to <target>'s binary, where the development runtime loads screens
# opened with aitbase_ui_open(plugin, "<screen>", ...). UI_DIR defaults to ./ui.
set(AITBASE_PLUGIN_SDK_DIR ${CMAKE_CURRENT_LIST_DIR}/.. CACHE INTERNAL "")

function(aitbase_plugin_add_ui target)
  cmake_parse_arguments(ARG "" "UI_DIR" "SCREENS" ${ARGN})
  if(NOT ARG_SCREENS)
    message(FATAL_ERROR "aitbase_plugin_add_ui: SCREENS is required")
  endif()
  if(NOT ARG_UI_DIR)
    set(ARG_UI_DIR ${CMAKE_CURRENT_SOURCE_DIR}/ui)
  endif()
  get_filename_component(ui_dir ${ARG_UI_DIR} ABSOLUTE)
  find_package(Python3 3.8 REQUIRED COMPONENTS Interpreter)
  file(GLOB_RECURSE ui_inputs CONFIGURE_DEPENDS ${ui_dir}/*)
  set(outputs)
  foreach(screen ${ARG_SCREENS})
    set(out ${CMAKE_CURRENT_BINARY_DIR}/${screen}.ui.json)
    add_custom_command(
      OUTPUT ${out}
      COMMAND Python3::Interpreter -I ${AITBASE_PLUGIN_SDK_DIR}/tools/plugin_ui_compile.py
              ${ui_dir} ${screen} ${out}
      DEPENDS ${ui_inputs}
              ${AITBASE_PLUGIN_SDK_DIR}/tools/plugin_ui_compile.py
              ${AITBASE_PLUGIN_SDK_DIR}/tools/plugin_ui_registry.py
              ${AITBASE_PLUGIN_SDK_DIR}/schema/plugin_ui_registry.json
      COMMENT "Compiling plugin screen ${screen}"
      VERBATIM
    )
    list(APPEND outputs ${out})
  endforeach()
  add_custom_target(${target}_ui ALL DEPENDS ${outputs})
  if(TARGET ${target})
    add_dependencies(${target} ${target}_ui)
  endif()
endfunction()
