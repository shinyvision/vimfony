<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class Channel extends AbstractChannel
{
    #[ORM\Column(type: 'string', nullable: true)]
    private ?string $customField = null;
}
