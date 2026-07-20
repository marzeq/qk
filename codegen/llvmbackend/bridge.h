#ifndef QK_LLVM_BACKEND_BRIDGE_H
#define QK_LLVM_BACKEND_BRIDGE_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

int qk_compile_llvm(
    const char *input,
    size_t input_size,
    const char *output_path,
    const char *optimized_ir_path,
    const char *target_triple,
    const char *cpu,
    const char *features,
    const char *target_abi,
    const char *optimization_level,
    const char *relocation_model,
    const char *code_model,
    int output_kind,
    int verbose,
    char **error_message);

void qk_dispose_error(char *error_message);

int qk_link_lld(
    const char *const *arguments,
    size_t argument_count,
    int verbose,
    char **error_message);

#ifdef __cplusplus
}
#endif

#endif
