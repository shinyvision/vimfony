<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class Order extends AbstractOrder
{
    /**
     * @var \DateTime
     */
    #[ORM\Column(type: "datetime")]
    private \DateTime $paymentCompletedAt;

    #[ORM\Column(type: "string")]
    private string $channel;

    #[ORM\Column]
    private int $notImportant;
}
