<?php
namespace App\Entity;

abstract class AbstractChannel
{
    private int $id;
    private string $code;
    private string $name;
    private bool $enabled;
}
