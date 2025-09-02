<?php

declare(strict_types=1);

namespace App;

use Symfony\Component\DependencyInjection\Attribute\Autoconfigure;

#[Autoconfigure(
    [
        [
            'name' => 'my_tag_name',
            'foo' => 'bar',
        ],
    ],
)]
class MyClass
{
    public function __invoke(): void
    {
        echo 'It works.';
    }
}
