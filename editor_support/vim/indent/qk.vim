" Vim indent file
" Language: qk

if exists("b:did_indent")
  finish
endif
let b:did_indent = 1

function! GetQkIndent() abort
  let l:previous = prevnonblank(v:lnum - 1)
  if l:previous == 0
    return 0
  endif

  let l:width = shiftwidth()
  let l:indent = indent(l:previous)
  let l:previous_line = substitute(getline(l:previous), '//.*$', '', '')
  let l:current_line = getline(v:lnum)

  " Indent after an opening delimiter or an unfinished expression.
  if l:previous_line =~# '[{[(]\s*$'
    let l:indent += l:width
  elseif l:previous_line =~# '\%([=,+\-*/%&|^]\|->\|=>\)\s*$'
    let l:indent += l:width
  endif

  " Closing delimiters align with the line containing their opener.
  if l:current_line =~# '^\s*[]})]'
    let l:indent -= l:width
  endif

  return max([l:indent, 0])
endfunction

setlocal indentexpr=GetQkIndent()
setlocal indentkeys=0{,0},0),0],!^F,o,O,e

let b:undo_indent = "setlocal indentexpr< indentkeys<"
