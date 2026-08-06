# tree-sitter-qk

Tree-sitter grammar, highlighting, and indentation queries for the QK
programming language. It recognizes both user source files (`.qk`) and
embedded standard-library source files (`.qks`).

Generate and validate the parser from this directory with:

```sh
tree-sitter generate
tree-sitter parse ../../examples/wc/main.qk ../../stdlib/sources/std/**/*.qks
```

Editors that accept a local Tree-sitter grammar can point at this directory and
use `queries/highlights.scm` for highlighting and `queries/indents.scm` for
indentation.

With `nvim-treesitter` on its `main` branch, enable QK indentation after the
parser has been installed:

```lua
vim.bo.indentexpr = "v:lua.require'nvim-treesitter'.indentexpr()"
```
