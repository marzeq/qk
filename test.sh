#!/usr/bin/env bash

generate_expected() {
  local force="$1"

  if [ ! -d "examples" ]; then
    echo "Directory 'examples' does not exist."
    return 1
  fi

  if [ ! -d "examples/expected_outputs" ]; then
    echo "Creating 'examples/expected_outputs' directory."
    mkdir -p "examples/expected_outputs"
  fi

  local files=(examples/*.qk)
  if [ ! -e "${files[0]}" ]; then
    echo "No .qk files found in 'examples'."
    return 0
  fi

  for file in "${files[@]}"; do
    local expected_output="examples/expected_outputs/$(basename "${file%.qk}.ssa")"
    if [ ! -f "$expected_output" ] || [ "$force" == "1" ]; then
      echo "Generating expected output for $file"
      go run main.go "$file" -irout "$expected_output"
      rm -f a.out
    else
      echo "Skipping $file, expected output already exists."
    fi
  done

  echo "Expected output generation complete."
}

run_tests() {
  if [ ! -d "examples" ]; then
    echo "Directory 'examples' does not exist."
    return 1
  fi

  if [ ! -d "examples/expected_outputs" ]; then
    echo "'examples/expected_outputs' directory does not exist. Run './test.sh gen' first."
    return 1
  fi

  local files=(examples/*.qk)
  if [ ! -e "${files[0]}" ]; then
    echo "No .qk files found in 'examples'."
    return 0
  fi

  local failed=0
  for file in "${files[@]}"; do
    local expected_output="examples/expected_outputs/$(basename "${file%.qk}.ssa")"
    if [ ! -f "$expected_output" ]; then
      echo "Missing expected output for $file. Run './test.sh gen' first."
      return 1
    fi

    go run main.go "$file" -irout /tmp/test.ssa
    if ! diff -q /tmp/test.ssa "$expected_output" > /dev/null; then
      echo "Test failed for $file"
      echo "Expected output:"
      cat "$expected_output"
      echo "Actual output:"
      cat /tmp/test.ssa
      failed=$((failed + 1))
    else
      echo "Test passed for $file"
    fi
    rm -f /tmp/test.ssa a.out
  done

  if [ $failed -ne 0 ]; then
    echo "$failed tests failed."
    return 1
  else
    echo "All tests passed."
    return 0
  fi
}

main() {
  local mode="${1:-run}"
  case "$mode" in
    gen|generate)
      local force=0
      if [[ "$2" == "-f" ]]; then
        force=1
      fi
      generate_expected "$force"
      ;;
    run|"")
      run_tests
      return $?
      ;;
    *)
      echo "Unknown mode: $mode. Use './test.sh run' or './test.sh gen'."
      return 1
      ;;
  esac
}

main "$@"
