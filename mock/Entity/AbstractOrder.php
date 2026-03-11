<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\MappedSuperclass]
abstract class AbstractOrder
{
    #[ORM\Column(type: "datetime")]
    private \DateTime $createdAt;
}
