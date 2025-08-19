# Vimfony, a simple utility LSP
This is a language server that adds some useful jumps for Symfony projects:
- Go-to twig templates with @Bundle support
- Go-to class from within yaml files
- Go-to service definitions for example @service_container

It uses your go-to definition keymapping (`gd` for most vimmers)

I have made this tool because I use [Sylius](https://github.com/Sylius/Sylius) a lot, which works with twig hooks and the amount of twig files can get overwhelming.
Also, PhpStorm has a Symfony plugin, so us NeoVim users should also have some nice things...

If you find a bug or want a feature, you can create an issue or a PR and maybe I’ll take a look at it. Maybe not.

## How to use
Build:
```bash
git clone https://github.com/shinyvision/vimfony.git
cd vimfony
go build
```

And then move the `vimfony` binary to somewhere in your $PATH

Configure LSP (Neovim):
```lua
local lspconfig = require 'lspconfig'
local configs = require 'lspconfig.configs'
local util = lspconfig.util

configs.vimfony = {
  default_config = {
    cmd = { "vimfony" },
    filetypes = { "php", "twig", "yaml" }, -- You can remove file types if you don’t like it, but then it won’t work in tose files
    root_dir = function(fname)
      return util.root_pattern("composer.json", ".git")(fname) or vim.fn.getcwd()
    end,
    single_file_support = true,
    init_options = {
      roots = { "templates" },
      container_xml_path = vim.fn.getcwd() .. "/var/cache/dev/App_KernelDevDebugContainer.xml", -- Where your container XML is
      vendor_dir = vim.fn.getcwd() .. "/vendor", -- Where your vendor directory is
    },
  },
}

lspconfig.vimfony.setup({})
```
By the way, I highly recommend purchasing a license for [Intelephense](https://intelephense.com/). It’s worth your 25 bucks.

## Finally
This is just a quick project because I was missing features in my workflow and I’m sharing it here with you. I am by no means an expert in language servers.

Also, I have no idea if this works for VSCode. I have never used VSCode, but I’ve heard it uses language servers in the same way as Neovim. Maybe it’ll work, maybe it won’t.
