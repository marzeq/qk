#include "bridge.h"

#include <cstdlib>
#include <cstring>
#include <memory>
#include <optional>
#include <string>

#include <llvm/ADT/StringRef.h>
#include <llvm/ADT/StringMap.h>
#include <llvm/Analysis/CGSCCPassManager.h>
#include <llvm/Analysis/LoopAnalysisManager.h>
#include <llvm/IR/LegacyPassManager.h>
#include <llvm/IR/Module.h>
#include <llvm/IR/PassManager.h>
#include <llvm/IR/Verifier.h>
#include <llvm/IRReader/IRReader.h>
#include <llvm/MC/TargetRegistry.h>
#include <llvm/Passes/PassBuilder.h>
#include <llvm/Support/CodeGen.h>
#include <llvm/Support/MemoryBuffer.h>
#include <llvm/Support/SourceMgr.h>
#include <llvm/Support/TargetSelect.h>
#include <llvm/Support/raw_ostream.h>
#include <llvm/Target/TargetMachine.h>
#include <llvm/Target/TargetOptions.h>
#include <llvm/TargetParser/Host.h>
#include <llvm/TargetParser/SubtargetFeature.h>

#include <clang/Basic/Diagnostic.h>
#include <clang/Basic/DiagnosticIDs.h>
#include <clang/Basic/DiagnosticOptions.h>
#include <clang/Driver/Compilation.h>
#include <clang/Driver/Driver.h>
#include <clang/Driver/Job.h>
#include <clang/Frontend/TextDiagnosticPrinter.h>

#include <lld/Common/Driver.h>

LLD_HAS_DRIVER(coff)
LLD_HAS_DRIVER(elf)
LLD_HAS_DRIVER(macho)
LLD_HAS_DRIVER(mingw)

namespace {

void set_error(char **destination, const std::string &message) {
  if (destination == nullptr) {
    return;
  }
  *destination = static_cast<char *>(std::malloc(message.size() + 1));
  if (*destination != nullptr) {
    std::memcpy(*destination, message.c_str(), message.size() + 1);
  }
}

std::string diagnostic_string(const llvm::SMDiagnostic &diagnostic) {
  std::string message;
  llvm::raw_string_ostream stream(message);
  diagnostic.print("qkc", stream);
  return stream.str();
}

std::optional<llvm::Reloc::Model> relocation_model(llvm::StringRef value) {
  if (value.empty() || value == "default") {
    return std::nullopt;
  }
  if (value == "static") {
    return llvm::Reloc::Static;
  }
  if (value == "pic") {
    return llvm::Reloc::PIC_;
  }
  if (value == "dynamic-no-pic") {
    return llvm::Reloc::DynamicNoPIC;
  }
  return std::nullopt;
}

std::optional<llvm::CodeModel::Model> code_model(llvm::StringRef value) {
  if (value.empty() || value == "default") {
    return std::nullopt;
  }
  if (value == "tiny") {
    return llvm::CodeModel::Tiny;
  }
  if (value == "small") {
    return llvm::CodeModel::Small;
  }
  if (value == "kernel") {
    return llvm::CodeModel::Kernel;
  }
  if (value == "medium") {
    return llvm::CodeModel::Medium;
  }
  if (value == "large") {
    return llvm::CodeModel::Large;
  }
  return std::nullopt;
}

llvm::OptimizationLevel ir_optimization_level(llvm::StringRef value) {
  if (value == "0") {
    return llvm::OptimizationLevel::O0;
  }
  if (value == "1" || value == "g") {
    return llvm::OptimizationLevel::O1;
  }
  if (value == "s") {
    return llvm::OptimizationLevel::Os;
  }
  if (value == "z") {
    return llvm::OptimizationLevel::Oz;
  }
  if (value == "3" || value == "fast") {
    return llvm::OptimizationLevel::O3;
  }
  return llvm::OptimizationLevel::O2;
}

llvm::CodeGenOptLevel codegen_optimization_level(llvm::StringRef value) {
  if (value == "0") {
    return llvm::CodeGenOptLevel::None;
  }
  if (value == "1" || value == "g") {
    return llvm::CodeGenOptLevel::Less;
  }
  if (value == "3" || value == "fast") {
    return llvm::CodeGenOptLevel::Aggressive;
  }
  return llvm::CodeGenOptLevel::Default;
}

} // namespace

extern "C" int qk_compile_llvm(
    const char *input,
    size_t input_size,
    const char *output_path,
    const char *optimized_ir_path,
    const char *target_triple,
    const char *cpu,
    const char *features,
    const char *target_abi,
    const char *optimization_level,
    const char *relocation_model_name,
    const char *code_model_name,
    int output_kind,
    int verbose,
    char **error_message) {
  if (error_message != nullptr) {
    *error_message = nullptr;
  }

  static const bool initialized = [] {
    llvm::InitializeAllTargetInfos();
    llvm::InitializeAllTargets();
    llvm::InitializeAllTargetMCs();
    llvm::InitializeAllAsmPrinters();
    llvm::InitializeAllAsmParsers();
    return true;
  }();
  (void)initialized;

  llvm::LLVMContext context;
  llvm::SMDiagnostic diagnostic;
  auto buffer = llvm::MemoryBuffer::getMemBufferCopy(
      llvm::StringRef(input, input_size), "<qk llvm module>");
  std::unique_ptr<llvm::Module> module =
      llvm::parseIR(buffer->getMemBufferRef(), diagnostic, context);
  if (!module) {
    set_error(error_message, diagnostic_string(diagnostic));
    return 1;
  }

  std::string triple = target_triple;
  if (triple.empty()) {
    triple = llvm::sys::getDefaultTargetTriple();
  }
  triple = llvm::Triple::normalize(triple);
  llvm::Triple target_triple_value(triple);

  std::string target_error;
  const llvm::Target *target =
      llvm::TargetRegistry::lookupTarget(target_triple_value, target_error);
  if (target == nullptr) {
    set_error(error_message, target_error);
    return 1;
  }

  llvm::TargetOptions target_options;
  target_options.UseInitArray = true;
  target_options.FunctionSections = true;
  target_options.DataSections = true;
  if (llvm::StringRef(optimization_level) == "fast") {
    target_options.NoInfsFPMath = true;
    target_options.NoNaNsFPMath = true;
    target_options.NoSignedZerosFPMath = true;
    target_options.AllowFPOpFusion = llvm::FPOpFusion::Fast;
  }
  if (target_abi[0] != '\0') {
    target_options.MCOptions.ABIName = target_abi;
  }

  std::string effective_cpu = cpu;
  std::string effective_features = features;
  if (effective_cpu == "native") {
    llvm::Triple host_triple(llvm::sys::getDefaultTargetTriple());
    if (host_triple.getArch() != target_triple_value.getArch()) {
      set_error(error_message, "-cpu native cannot be used with a different target architecture");
      return 1;
    }
    effective_cpu = llvm::sys::getHostCPUName().str();
    if (effective_features.empty()) {
      llvm::StringMap<bool> host_features = llvm::sys::getHostCPUFeatures();
      llvm::SubtargetFeatures feature_list;
      for (const auto &feature : host_features) {
        feature_list.AddFeature(feature.getKey(), feature.getValue());
      }
      effective_features = feature_list.getString();
    }
  }

  std::unique_ptr<llvm::TargetMachine> target_machine(
      target->createTargetMachine(
          target_triple_value,
          effective_cpu,
          effective_features,
          target_options,
          relocation_model(relocation_model_name),
          code_model(code_model_name),
          codegen_optimization_level(optimization_level)));
  if (!target_machine) {
    set_error(error_message, "could not create LLVM target machine for " + triple);
    return 1;
  }

  module->setTargetTriple(llvm::Triple(triple));
  module->setDataLayout(target_machine->createDataLayout());

  std::string verification_error;
  llvm::raw_string_ostream verification_stream(verification_error);
  if (llvm::verifyModule(*module, &verification_stream)) {
    set_error(error_message, verification_stream.str());
    return 1;
  }

  llvm::LoopAnalysisManager loop_analyses;
  llvm::FunctionAnalysisManager function_analyses;
  llvm::CGSCCAnalysisManager cgscc_analyses;
  llvm::ModuleAnalysisManager module_analyses;
  llvm::PassBuilder pass_builder(target_machine.get());
  pass_builder.registerModuleAnalyses(module_analyses);
  pass_builder.registerCGSCCAnalyses(cgscc_analyses);
  pass_builder.registerFunctionAnalyses(function_analyses);
  pass_builder.registerLoopAnalyses(loop_analyses);
  pass_builder.crossRegisterProxies(
      loop_analyses, function_analyses, cgscc_analyses, module_analyses);

  llvm::ModulePassManager passes;
  llvm::ModulePassManager global_dce;
  if (auto parse_error = pass_builder.parsePassPipeline(global_dce, "globaldce")) {
    set_error(error_message, llvm::toString(std::move(parse_error)));
    return 1;
  }
  passes.addPass(std::move(global_dce));
  if (optimization_level[0] != '0') {
    passes.addPass(pass_builder.buildPerModuleDefaultPipeline(
        ir_optimization_level(optimization_level)));
  }
  passes.run(*module, module_analyses);

  if (optimized_ir_path[0] != '\0') {
    std::error_code ir_error;
    llvm::raw_fd_ostream ir_output(optimized_ir_path, ir_error);
    if (ir_error) {
      set_error(error_message, "could not write optimized LLVM IR: " + ir_error.message());
      return 1;
    }
    module->print(ir_output, nullptr);
  }

  std::error_code output_error;
  llvm::raw_fd_ostream output(output_path, output_error, llvm::sys::fs::OF_None);
  if (output_error) {
    set_error(error_message, "could not open backend output: " + output_error.message());
    return 1;
  }

  llvm::CodeGenFileType file_type = output_kind == 1
      ? llvm::CodeGenFileType::AssemblyFile
      : llvm::CodeGenFileType::ObjectFile;
  llvm::legacy::PassManager codegen_passes;
  if (target_machine->addPassesToEmitFile(codegen_passes, output, nullptr, file_type)) {
    set_error(error_message, "LLVM target cannot emit the requested output type");
    return 1;
  }
  if (verbose) {
    llvm::errs() << "libLLVM: " << triple << " " << optimization_level << " -> "
                 << output_path << "\n";
  }
  codegen_passes.run(*module);
  output.flush();
  return 0;
}

extern "C" void qk_dispose_error(char *error_message) {
  std::free(error_message);
}

extern "C" int qk_link_lld(
    const char *const *arguments,
    size_t argument_count,
    int verbose,
    char **error_message) {
  if (error_message != nullptr) {
    *error_message = nullptr;
  }

  std::string diagnostics;
  llvm::raw_string_ostream diagnostic_stream(diagnostics);
  clang::DiagnosticOptions diagnostic_options;
  auto diagnostic_ids = llvm::makeIntrusiveRefCnt<clang::DiagnosticIDs>();
  clang::TextDiagnosticPrinter diagnostic_printer(
      diagnostic_stream, diagnostic_options);
  clang::DiagnosticsEngine diagnostic_engine(
      diagnostic_ids, diagnostic_options, &diagnostic_printer, false);

  clang::driver::Driver driver(
      "clang", llvm::sys::getDefaultTargetTriple(), diagnostic_engine);
  driver.setCheckInputsExist(false);

  llvm::SmallVector<const char *, 32> driver_arguments;
  driver_arguments.push_back("clang");
  driver_arguments.append(arguments, arguments + argument_count);
  std::unique_ptr<clang::driver::Compilation> compilation(
      driver.BuildCompilation(driver_arguments));
  diagnostic_stream.flush();
  if (!compilation || diagnostic_engine.hasErrorOccurred()) {
    set_error(error_message, diagnostics.empty()
        ? "Clang driver could not construct the LLD link job"
        : diagnostics);
    return 1;
  }
  if (!diagnostics.empty()) {
    llvm::errs() << diagnostics;
  }

  const auto &jobs = compilation->getJobs().getJobs();
  if (jobs.size() != 1) {
    set_error(error_message, "Clang driver produced an unexpected number of link jobs");
    return 1;
  }
  const clang::driver::Command &command = *jobs.front();

  llvm::SmallVector<const char *, 64> lld_arguments;
  lld_arguments.push_back(command.getExecutable());
  lld_arguments.append(command.getArguments());
  if (verbose) {
    llvm::errs() << "> libLLD";
    for (const char *argument : lld_arguments) {
      llvm::errs() << " " << argument;
    }
    llvm::errs() << "\n";
  }

  std::string linker_stdout;
  std::string linker_stderr;
  llvm::raw_string_ostream stdout_stream(linker_stdout);
  llvm::raw_string_ostream stderr_stream(linker_stderr);
  const lld::DriverDef drivers[] = {
      {lld::Gnu, &lld::elf::link},
      {lld::MinGW, &lld::mingw::link},
      {lld::WinLink, &lld::coff::link},
      {lld::Darwin, &lld::macho::link},
  };
  lld::Result result = lld::lldMain(
      lld_arguments, stdout_stream, stderr_stream, drivers);
  stdout_stream.flush();
  stderr_stream.flush();
  if (!linker_stdout.empty()) {
    llvm::outs() << linker_stdout;
  }
  if (result.retCode != 0) {
    std::string message = linker_stderr;
    if (!result.canRunAgain) {
      message += "\nLLD reported that it cannot safely be invoked again";
    }
    set_error(error_message, message);
    return result.retCode;
  }
  if (!linker_stderr.empty()) {
    llvm::errs() << linker_stderr;
  }
  return 0;
}
