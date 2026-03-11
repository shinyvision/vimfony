<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

trait TimestampableTrait
{
    #[ORM\Column(type: "datetime")]
    private \DateTime $createdAt;

    #[ORM\Column(type: "datetime", nullable: true)]
    private ?\DateTime $updatedAt = null;
}
