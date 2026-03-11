<?php
namespace App\Service;

use App\Entity\Order;

class OrderService
{
    private $entityManager;

    public function processOrders()
    {
        $paidOrders = $this->entityManager->getRepository(Order::class)
            ->createQueryBuilder('o')
            ->where('o.paymentCompletedAt >= :today AND o.paymentCompletedAt < :tomorrow')
            ->andWhere('o.channel = :channel')
            ->setParameters(new ArrayCollection([new Parameter('today', $today), new Parameter('tomorrow', $tomorrow), new Parameter('channel', $channel)]))
            ->getQuery()
            ->getResult()
        ;
    }
}
