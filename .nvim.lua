vim.filetype.add({
  extension = {
    qk = "qk",
  },
})

vim.api.nvim_create_autocmd("FileType", {
  pattern = "qk",
  callback = function()
    vim.opt_local.commentstring = "// %s"
  end,
})
