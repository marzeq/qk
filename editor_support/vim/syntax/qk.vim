" Vim syntax file
" Language: qk

if exists("b:current_syntax")
  finish
endif

syn case match

" Comments
syn keyword qkTodo TODO FIXME XXX NOTE contained
syn match qkLineComment "//.*$" contains=qkTodo,@Spell
syn region qkBlockComment start="/\*" end="\*/" contains=qkTodo,@Spell

" Literals
syn match qkEscape contained "\\[\\\"nrtbfva0]"
syn match qkInvalidEscape contained "\\[^\\\"nrtbfva0]"
syn region qkString start=+"+ skip=+\\\\\|\\"+ end=+"+ contains=qkEscape,qkInvalidEscape
syn region qkCString start=+\<c"+ skip=+\\\\\|\\"+ end=+"+ contains=qkEscape,qkInvalidEscape
syn region qkCharacter start=+'+ skip=+\\\\\|\\'+ end=+'+ contains=qkEscape,qkInvalidEscape

syn match qkNumber "\<0[bB][01]\+\>"
syn match qkNumber "\<0[oO][0-7]\+\>"
syn match qkNumber "\<0[xX][0-9a-fA-F]\+\>"
syn match qkFloat "\<\d\+\.\d*\>\|\(^\|\W\)\zs\.\d\+\>"
syn match qkNumber "\<\d\+\>"
syn keyword qkBoolean true false
syn keyword qkConstant nil

" Language words
syn keyword qkDeclaration let mut pub module import
syn keyword qkTypeKeyword type alias struct union enum opaque
syn keyword qkConditional if else given when
syn keyword qkRepeat for in
syn keyword qkStatement break continue return defer
syn keyword qkOperator and or not as
syn keyword qkBuiltin sizeof alignof offsetof len

" Function and module attributes.
syn match qkAttribute "@[A-Za-z_][A-Za-z0-9_]*"

hi def link qkTodo Todo
hi def link qkLineComment Comment
hi def link qkBlockComment Comment
hi def link qkEscape SpecialChar
hi def link qkInvalidEscape Error
hi def link qkString String
hi def link qkCString String
hi def link qkCharacter Character
hi def link qkNumber Number
hi def link qkFloat Float
hi def link qkBoolean Boolean
hi def link qkConstant Constant
hi def link qkDeclaration Statement
hi def link qkTypeKeyword Keyword
hi def link qkConditional Conditional
hi def link qkRepeat Repeat
hi def link qkStatement Statement
hi def link qkOperator Operator
hi def link qkBuiltin Function
hi def link qkAttribute PreProc

let b:current_syntax = "qk"
