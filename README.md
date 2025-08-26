<p align="center">
    <img src="https://raw.githubusercontent.com/shinyvision/vimfony/main/.github/assets/vimfony_sm.png" alt="Vimfony Logo">
</p>

This is a language server that adds some useful jumps for Symfony projects.

It uses your go-to definition keymapping (`gd` for most vimmers)

I have made this tool because I use [Sylius](https://github.com/Sylius/Sylius) a lot, which works with twig hooks and the amount of twig files can get overwhelming.
Also, PhpStorm has a Symfony plugin, so us NeoVim users should also have some nice things...

If you find a bug or want a feature, you can create an issue or a PR. I’ll probably take a look at it, but no promises.
If you do create a PR, then please don’t try to implement a PHP parser, because all Go libraries I’ve found seem to be abandoned. For now, we’ll just simply stream files and rely on string matching to get the work done, and it works pretty reliable so far.

## Features
- `gd` Twig templates with @Bundle support
- `gd` Twig functions
- `gd` class from within yaml files
- `gd` service definitions for example @service_container
- Autocomplete service names (works in yaml files and autoconfigure php attributes)

## Planned features
These features are not yet implemented but would be useful:
(feel free to create a PR if you want to contribute)
- Support for XML service configuration
- Autocomplete Twig files
- `gd` Twig components
- Version checker & updater (`vimfony update`)

## How to use
You can download a release for your OS and CPU or build from source:
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
      return util.root_pattern("composer.json", ".git")(fname)
    end,
    single_file_support = true,
    init_options = {
      roots = { "templates" },
      container_xml_path = util.root_pattern("composer.json", ".git")() .. "/var/cache/dev/App_KernelDevDebugContainer.xml", -- Where your container XML is
      vendor_dir = util.root_pattern("composer.json", ".git")() .. "/vendor", -- Where your vendor directory is
      -- Optional:
      -- php_path = "/usr/bin/php",
    },
  },
}

lspconfig.vimfony.setup({})
```

If you use this project and like what it does, then please **give it a star** on Github.

PS. I highly recommend purchasing a license for [Intelephense](https://intelephense.com/). It’s worth your 25 bucks.

## Finally
This is just a quick project because I was missing features in my workflow and I’m sharing it here with you. I am by no means an expert in language servers.

Also, I have no idea if this works for VSCode. I have never used VSCode, but I’ve heard it uses language servers in the same way as Neovim. Maybe it’ll work, maybe it won’t.
