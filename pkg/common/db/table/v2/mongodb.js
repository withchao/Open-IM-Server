db.demo.updateMany(
    {
        "user_id": "2000"
    },
    [
        {
            $addFields: {
                isFriendFound: {
                    $in: ["1000", "$friends.friend_user_id"]
                }
            }
        },
        {
            $set: {
                update_version: {
                    $cond: {
                        if: "$isFriendFound",
                        then: "$update_version",
                        else: { $add: ["$update_version", 1] }
                    }
                }
            }
        },
        {
            $set: {
                friends: {
                    $cond: {
                        if: "$isFriendFound",
                        then: "$friends",
                        else: {
                            $concatArrays: [
                                "$friends",
                                [
                                    {
                                        friend_user_id: "1000",
                                        nickname: "new_friend_nickname",
                                        face_url: "new_friend_face_url",
                                        remark: "new_friend_remark",
                                        create_time: new Date(),
                                        add_source: 1,
                                        operator_user_id: "new_operator_user_id",
                                        ex: "new_ex",
                                        is_pinned: false,
                                        update_version: "$update_version",
                                        deleted: false
                                    }
                                ]
                            ]
                        }
                    }
                },
            }
        },
        {
            $unset: "isFriendFound"
        }
    ]
)
