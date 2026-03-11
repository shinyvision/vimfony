<?php
namespace App\Entity;

use Doctrine\Common\Collections\Collection;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class Channel extends AbstractChannel
{
    #[ORM\Column(type: 'string', nullable: true)]
    private ?string $customField = null;

    #[ORM\ManyToMany]
    private Collection $currencies;
}
